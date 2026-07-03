package entstore

import (
	"context"
	"fmt"

	"github.com/nzlov/anycode/internal/domain/process"
	"github.com/nzlov/anycode/internal/domain/redaction"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	entprocessrun "github.com/nzlov/anycode/internal/infra/entstore/ent/processrun"
)

var _ process.Repository = (*ProcessRepository)(nil)

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

func (r *ProcessRepository) SaveEvent(ctx context.Context, event process.Event) error {
	create := r.client.ProcessEvent.Create().
		SetID(event.ID).
		SetSessionID(string(event.SessionID)).
		SetEventID(event.EventID).
		SetType(event.Type).
		SetPayload(redaction.Map(payloadOrEmpty(event.Payload)))
	if event.ProcessRunID != nil {
		create.SetProcessRunID(string(*event.ProcessRunID))
	}
	if !event.CreatedAt.IsZero() {
		create.SetCreatedAt(event.CreatedAt)
	}
	if err := create.Exec(ctx); err != nil {
		return fmt.Errorf("save process event: %w", err)
	}
	return nil
}

func activeProcessStatuses() []string {
	return []string{
		string(process.StatusStarting),
		string(process.StatusRunning),
		string(process.StatusWaitingUser),
		string(process.StatusStopping),
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
