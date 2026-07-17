package entstore

import (
	"context"
	"fmt"

	"github.com/nzlov/anycode/internal/application/port"
	"github.com/nzlov/anycode/internal/domain/process"
	domainsession "github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	entsession "github.com/nzlov/anycode/internal/infra/entstore/ent/session"
)

func (s *Store) ClaimExecution(ctx context.Context, input port.ExecutionClaimInput) (port.ExecutionClaimResult, error) {
	var result port.ExecutionClaimResult
	err := s.Do(ctx, func(ctx context.Context, tx port.Tx) error {
		var err error
		result, err = tx.ClaimExecution(ctx, input)
		return err
	})
	return result, err
}

func (t transaction) ClaimExecution(ctx context.Context, input port.ExecutionClaimInput) (port.ExecutionClaimResult, error) {
	return claimExecution(ctx, t.client, input)
}

func (t transaction) PrepareClose(ctx context.Context, input port.ClosePreparationInput) (port.ClosePreparationResult, error) {
	return prepareClose(ctx, t.client, input)
}

func claimExecution(ctx context.Context, client *ent.Client, input port.ExecutionClaimInput) (port.ExecutionClaimResult, error) {
	sessions := NewSessionRepository(client)
	processes := NewProcessRepository(client)
	current, err := sessions.Find(ctx, input.ExpectedSession.ID)
	if err != nil {
		return port.ExecutionClaimResult{}, fmt.Errorf("find execution claim session: %w", err)
	}
	if active, found, err := processes.FindActiveBySession(ctx, process.SessionID(current.ID)); err != nil {
		return port.ExecutionClaimResult{}, err
	} else if found {
		return port.ExecutionClaimResult{Status: port.ExecutionAlreadyActive, Session: current, ActiveRun: &active}, nil
	}
	if !current.MatchesLifecycleSnapshot(input.ExpectedSession) {
		return port.ExecutionClaimResult{Status: port.ExecutionStale, Session: current}, nil
	}
	if input.MaxActive > 0 {
		count, err := processes.CountActive(ctx)
		if err != nil {
			return port.ExecutionClaimResult{}, err
		}
		if count >= input.MaxActive {
			return port.ExecutionClaimResult{Status: port.ExecutionAtCapacity, Session: current}, nil
		}
	}
	if err := processes.CreateRun(ctx, input.Run); err != nil {
		if ent.IsConstraintError(err) {
			active, found, findErr := processes.FindActiveBySession(ctx, process.SessionID(current.ID))
			if findErr == nil && found {
				return port.ExecutionClaimResult{Status: port.ExecutionAlreadyActive, Session: current, ActiveRun: &active}, nil
			}
		}
		return port.ExecutionClaimResult{}, err
	}
	updated, err := updateClaimedSessionState(ctx, client, input.ExpectedSession, input.StartingSession)
	if err != nil {
		return port.ExecutionClaimResult{}, err
	}
	if !updated {
		if err := client.ProcessRun.DeleteOneID(string(input.Run.ID)).Exec(ctx); err != nil {
			return port.ExecutionClaimResult{}, fmt.Errorf("rollback stale execution claim run: %w", err)
		}
		latest, findErr := sessions.Find(ctx, input.ExpectedSession.ID)
		if findErr != nil {
			return port.ExecutionClaimResult{}, findErr
		}
		return port.ExecutionClaimResult{Status: port.ExecutionStale, Session: latest}, nil
	}
	return port.ExecutionClaimResult{Status: port.ExecutionClaimed, Session: input.StartingSession}, nil
}

func prepareClose(ctx context.Context, client *ent.Client, input port.ClosePreparationInput) (port.ClosePreparationResult, error) {
	sessions := NewSessionRepository(client)
	processes := NewProcessRepository(client)
	current, err := sessions.Find(ctx, input.ExpectedSession.ID)
	if err != nil {
		return port.ClosePreparationResult{}, fmt.Errorf("find close preparation session: %w", err)
	}
	if current.Status == domainsession.StatusClosed {
		return port.ClosePreparationResult{Status: port.CloseAlreadyClosed, Session: current}, nil
	}
	if active, found, err := processes.FindActiveBySession(ctx, process.SessionID(current.ID)); err != nil {
		return port.ClosePreparationResult{}, err
	} else if found {
		return port.ClosePreparationResult{Status: port.CloseActive, Session: current, ActiveRun: &active}, nil
	}
	if !current.MatchesLifecycleSnapshot(input.ExpectedSession) {
		return port.ClosePreparationResult{Status: port.CloseStale, Session: current}, nil
	}
	updated, err := updateClaimedSessionState(ctx, client, input.ExpectedSession, input.ClosingSession)
	if err != nil {
		return port.ClosePreparationResult{}, err
	}
	if !updated {
		latest, findErr := sessions.Find(ctx, input.ExpectedSession.ID)
		if findErr != nil {
			return port.ClosePreparationResult{}, findErr
		}
		if active, found, findActiveErr := processes.FindActiveBySession(ctx, process.SessionID(latest.ID)); findActiveErr != nil {
			return port.ClosePreparationResult{}, findActiveErr
		} else if found {
			return port.ClosePreparationResult{Status: port.CloseActive, Session: latest, ActiveRun: &active}, nil
		}
		return port.ClosePreparationResult{Status: port.CloseStale, Session: latest}, nil
	}
	return port.ClosePreparationResult{Status: port.ClosePrepared, Session: input.ClosingSession}, nil
}

func updateClaimedSessionState(ctx context.Context, client *ent.Client, expected domainsession.Session, starting domainsession.Session) (bool, error) {
	update := client.Session.Update().
		Where(
			entsession.IDEQ(string(expected.ID)),
			entsession.StatusEQ(string(expected.Status)),
			entsession.UpdatedAtEQ(expected.UpdatedAt),
		).
		SetStatus(string(starting.Status)).
		SetQueueKind(string(starting.Queue.Kind)).
		SetQueuePriority(string(normalizeQueuePriority(starting.Queue.Priority))).
		SetQueueInitialStart(starting.Queue.InitialStart).
		SetQueueReviewAfterReuseFailure(starting.Queue.ReviewAfterReuseFailure).
		SetQueuePrompt(starting.Queue.Prompt).
		SetQueueResumeCodexSessionID(starting.Queue.ResumeCodexSessionID).
		SetQueueResumeOfProcessRunID(starting.Queue.ResumeOfProcessRunID).
		SetQueueAnswerBatchID(starting.Queue.AnswerBatchID).
		SetUpdatedAt(starting.UpdatedAt)
	if starting.Queue.NodeRunID == nil {
		update.SetQueueNodeRunID("")
	} else {
		update.SetQueueNodeRunID(string(*starting.Queue.NodeRunID))
	}
	if starting.QueuedAt == nil {
		update.ClearQueuedAt()
	} else {
		update.SetQueuedAt(*starting.QueuedAt)
	}
	if starting.LastRunAt == nil {
		update.ClearLastRunAt()
	} else {
		update.SetLastRunAt(*starting.LastRunAt)
	}
	if starting.CloseReason == nil {
		update.ClearCloseReason()
	} else {
		update.SetCloseReason(string(*starting.CloseReason))
	}
	count, err := update.Save(ctx)
	if err != nil {
		return false, fmt.Errorf("claim session execution: %w", err)
	}
	return count == 1, nil
}
