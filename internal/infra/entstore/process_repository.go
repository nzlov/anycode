package entstore

import (
	"context"
	"fmt"
	"strings"

	"github.com/nzlov/anycode/internal/domain/process"
	"github.com/nzlov/anycode/internal/domain/redaction"
	domainsession "github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	entprocessevent "github.com/nzlov/anycode/internal/infra/entstore/ent/processevent"
	entprocessrun "github.com/nzlov/anycode/internal/infra/entstore/ent/processrun"
	entsession "github.com/nzlov/anycode/internal/infra/entstore/ent/session"
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

func (r *ProcessRepository) LatestCodexSessionID(ctx context.Context, sessionID process.SessionID) (string, error) {
	rows, err := r.client.ProcessEvent.Query().
		Where(
			entprocessevent.SessionIDEQ(string(sessionID)),
			entprocessevent.TypeEQ("thread.started"),
		).
		Order(ent.Desc(entprocessevent.FieldCreatedAt), ent.Desc(entprocessevent.FieldID)).
		All(ctx)
	if err != nil {
		return "", fmt.Errorf("list latest codex session events: %w", err)
	}
	for _, row := range rows {
		if id := codexSessionIDFromProcessPayload(row.Payload); id != "" {
			return id, nil
		}
	}
	return "", nil
}

func codexSessionIDFromProcessPayload(payload map[string]any) string {
	for _, key := range codexSessionIDPayloadKeys() {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	if msg, ok := payload["msg"].(map[string]any); ok {
		for _, key := range codexSessionIDPayloadKeys() {
			if value, ok := msg[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
	}
	return ""
}

func codexSessionIDPayloadKeys() []string {
	return []string{"session_id", "sessionId", "codex_session_id", "codexSessionId", "thread_id", "threadId", "conversation_id", "conversationId"}
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
