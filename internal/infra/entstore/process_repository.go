package entstore

import (
	"context"
	"fmt"
	"time"

	"github.com/nzlov/anycode/internal/domain/process"
	domainsession "github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	entprocessrun "github.com/nzlov/anycode/internal/infra/entstore/ent/processrun"
	entsession "github.com/nzlov/anycode/internal/infra/entstore/ent/session"
)

var _ process.Repository = (*ProcessRepository)(nil)
var _ process.HistoricalRunFinder = (*ProcessRepository)(nil)

type ProcessRepository struct {
	client *ent.Client
}

func NewProcessRepository(client *ent.Client) *ProcessRepository {
	return &ProcessRepository{client: client}
}

func (r *ProcessRepository) CreateRun(ctx context.Context, run process.Run) error {
	create := r.client.ProcessRun.Create().
		SetID(string(run.ID)).
		SetSessionID(string(run.SessionID)).
		SetStatus(string(run.Status)).
		SetCodexSessionID(run.CodexSessionID).
		SetFailureReason(run.FailureReason)
	if run.NodeRunID != nil {
		create.SetNodeRunID(string(*run.NodeRunID))
	}
	if run.PID != nil {
		create.SetPid(*run.PID)
	}
	if run.ResumeOf != nil {
		create.SetResumeOf(string(*run.ResumeOf))
	}
	if run.ExitCode != nil {
		create.SetExitCode(*run.ExitCode)
	}
	if !run.StartedAt.IsZero() {
		create.SetStartedAt(run.StartedAt)
	}
	if run.FinishedAt != nil {
		create.SetFinishedAt(*run.FinishedAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("create process run: %w", err)
	}
	return nil
}

func (r *ProcessRepository) HasAnyBySession(ctx context.Context, sessionID process.SessionID) (bool, error) {
	exists, err := r.client.ProcessRun.Query().
		Where(entprocessrun.SessionIDEQ(string(sessionID))).
		Exist(ctx)
	if err != nil {
		return false, fmt.Errorf("check process run history: %w", err)
	}
	return exists, nil
}

func (r *ProcessRepository) FindRun(ctx context.Context, id process.RunID) (process.Run, error) {
	row, err := r.client.ProcessRun.Get(ctx, string(id))
	if err != nil {
		return process.Run{}, fmt.Errorf("find process run: %w", err)
	}
	return toDomainProcessRun(row), nil
}

func (r *ProcessRepository) FindLatestRunBySessionBefore(ctx context.Context, sessionID process.SessionID, before time.Time) (process.Run, bool, error) {
	row, err := r.client.ProcessRun.Query().
		Where(
			entprocessrun.SessionIDEQ(string(sessionID)),
			entprocessrun.StartedAtLTE(before),
		).
		Order(ent.Desc(entprocessrun.FieldStartedAt), ent.Desc(entprocessrun.FieldID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return process.Run{}, false, nil
		}
		return process.Run{}, false, fmt.Errorf("find historical process run: %w", err)
	}
	return toDomainProcessRun(row), true, nil
}

func (r *ProcessRepository) FindActiveBySession(ctx context.Context, sessionID process.SessionID) (process.Run, bool, error) {
	row, err := r.client.ProcessRun.Query().
		Where(
			entprocessrun.SessionIDEQ(string(sessionID)),
			entprocessrun.StatusIn(activeProcessStatuses()...),
		).
		Order(ent.Desc(entprocessrun.FieldStartedAt), ent.Desc(entprocessrun.FieldID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return process.Run{}, false, nil
		}
		return process.Run{}, false, fmt.Errorf("find active process run: %w", err)
	}
	return toDomainProcessRun(row), true, nil
}

func (r *ProcessRepository) FindLatestBySession(ctx context.Context, sessionID process.SessionID) (process.Run, bool, error) {
	row, err := r.client.ProcessRun.Query().
		Where(entprocessrun.SessionIDEQ(string(sessionID))).
		Order(ent.Desc(entprocessrun.FieldStartedAt), ent.Desc(entprocessrun.FieldID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return process.Run{}, false, nil
		}
		return process.Run{}, false, fmt.Errorf("find latest process run: %w", err)
	}
	return toDomainProcessRun(row), true, nil
}

func (r *ProcessRepository) CountActive(ctx context.Context) (int, error) {
	sessionIDs, err := r.client.Session.Query().
		Where(entsession.StatusIn(concurrencySessionStatuses()...)).
		Select(entsession.FieldID).
		Strings(ctx)
	if err != nil {
		return 0, fmt.Errorf("list active sessions for process count: %w", err)
	}
	if len(sessionIDs) == 0 {
		return 0, nil
	}
	count, err := r.client.ProcessRun.Query().
		Where(
			entprocessrun.SessionIDIn(sessionIDs...),
			entprocessrun.StatusIn(concurrencyProcessStatuses()...),
		).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count active process runs: %w", err)
	}
	return count, nil
}

func (r *ProcessRepository) MarkWaitingUser(ctx context.Context, id process.RunID) error {
	if err := r.client.ProcessRun.UpdateOneID(string(id)).
		SetStatus(string(process.StatusWaitingUser)).
		Exec(ctx); err != nil {
		return fmt.Errorf("mark process waiting user: %w", err)
	}
	return nil
}

func (r *ProcessRepository) MarkRunning(ctx context.Context, id process.RunID, pid int, codexSessionID string) error {
	if err := r.client.ProcessRun.UpdateOneID(string(id)).
		SetStatus(string(process.StatusRunning)).
		SetPid(pid).
		SetCodexSessionID(codexSessionID).
		Exec(ctx); err != nil {
		return fmt.Errorf("mark process running: %w", err)
	}
	return nil
}

func (r *ProcessRepository) MarkStopping(ctx context.Context, id process.RunID) error {
	if err := r.client.ProcessRun.UpdateOneID(string(id)).
		SetStatus(string(process.StatusStopping)).
		Exec(ctx); err != nil {
		return fmt.Errorf("mark process stopping: %w", err)
	}
	return nil
}

func (r *ProcessRepository) MarkExited(ctx context.Context, id process.RunID, result process.ExitResult) error {
	update := r.client.ProcessRun.UpdateOneID(string(id)).
		SetStatus(string(process.StatusExited)).
		SetFailureReason(result.FailureReason).
		SetFinishedAt(result.FinishedAt)
	if result.ExitCode == nil {
		update.ClearExitCode()
	} else {
		update.SetExitCode(*result.ExitCode)
	}
	if err := update.Exec(ctx); err != nil {
		return fmt.Errorf("mark process exited: %w", err)
	}
	return nil
}

func (r *ProcessRepository) CodexSessionIDs(ctx context.Context, sessionID process.SessionID) ([]string, error) {
	rows, err := r.client.ProcessRun.Query().
		Where(
			entprocessrun.SessionIDEQ(string(sessionID)),
			entprocessrun.CodexSessionIDNEQ(""),
		).
		Order(ent.Asc(entprocessrun.FieldStartedAt), ent.Asc(entprocessrun.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list codex session ids: %w", err)
	}
	seen := map[string]struct{}{}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if _, ok := seen[row.CodexSessionID]; ok {
			continue
		}
		seen[row.CodexSessionID] = struct{}{}
		ids = append(ids, row.CodexSessionID)
	}
	return ids, nil
}

func activeProcessStatuses() []string {
	return []string{
		string(process.StatusStarting),
		string(process.StatusRunning),
		string(process.StatusWaitingUser),
		string(process.StatusStopping),
	}
}

func concurrencyProcessStatuses() []string {
	return []string{
		string(process.StatusStarting),
		string(process.StatusRunning),
	}
}

func concurrencySessionStatuses() []string {
	return []string{
		string(domainsession.StatusStarting),
		string(domainsession.StatusRunning),
	}
}

func toDomainProcessRun(row *ent.ProcessRun) process.Run {
	var nodeRunID *process.NodeRunID
	if row.NodeRunID != nil {
		value := process.NodeRunID(*row.NodeRunID)
		nodeRunID = &value
	}
	var resumeOf *process.RunID
	if row.ResumeOf != nil {
		value := process.RunID(*row.ResumeOf)
		resumeOf = &value
	}
	return process.Run{
		ID:             process.RunID(row.ID),
		SessionID:      process.SessionID(row.SessionID),
		NodeRunID:      nodeRunID,
		Status:         process.Status(row.Status),
		PID:            row.Pid,
		CodexSessionID: row.CodexSessionID,
		ResumeOf:       resumeOf,
		ExitCode:       row.ExitCode,
		FailureReason:  row.FailureReason,
		StartedAt:      row.StartedAt,
		FinishedAt:     row.FinishedAt,
	}
}
