package entstore

import (
	"context"
	"time"

	"github.com/nzlov/anycode/internal/domain/agentprofile"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	"github.com/nzlov/anycode/internal/infra/entstore/ent/processrun"
	"github.com/nzlov/anycode/internal/infra/entstore/ent/promptappend"
	entsession "github.com/nzlov/anycode/internal/infra/entstore/ent/session"
)

type AgentProfileRepository struct{ client *ent.Client }

func (s *Store) AgentProfiles() *AgentProfileRepository {
	return &AgentProfileRepository{client: s.client}
}

func (r *AgentProfileRepository) Get(ctx context.Context) (agentprofile.Profile, error) {
	row, err := r.client.AgentProfile.Get(ctx, "global")
	if ent.IsNotFound(err) {
		return agentprofile.Default(), nil
	}
	if err != nil {
		return agentprofile.Profile{}, err
	}
	return agentprofile.Profile{Configuration: row.Configuration, Analysis: row.Analysis}, nil
}

func (r *AgentProfileRepository) SaveConfiguration(ctx context.Context, cfg agentprofile.Configuration) error {
	err := r.client.AgentProfile.UpdateOneID("global").SetConfiguration(cfg).Exec(ctx)
	if !ent.IsNotFound(err) {
		return err
	}
	err = r.client.AgentProfile.Create().SetID("global").SetConfiguration(cfg).SetAnalysis(agentprofile.Default().Analysis).Exec(ctx)
	if ent.IsConstraintError(err) {
		return r.client.AgentProfile.UpdateOneID("global").SetConfiguration(cfg).Exec(ctx)
	}
	return err
}

func (r *AgentProfileRepository) SaveAnalysis(ctx context.Context, analysis agentprofile.Analysis) error {
	err := r.client.AgentProfile.UpdateOneID("global").SetAnalysis(analysis).Exec(ctx)
	if !ent.IsNotFound(err) {
		return err
	}
	err = r.client.AgentProfile.Create().SetID("global").SetConfiguration(agentprofile.Default().Configuration).SetAnalysis(analysis).Exec(ctx)
	if ent.IsConstraintError(err) {
		return r.client.AgentProfile.UpdateOneID("global").SetAnalysis(analysis).Exec(ctx)
	}
	return err
}

func (r *AgentProfileRepository) ListPromptAppends(ctx context.Context, since, until time.Time) ([]agentprofile.PromptAppend, error) {
	rows, err := r.client.PromptAppend.Query().Where(promptappend.CreatedAtGT(since), promptappend.CreatedAtLTE(until), promptappend.BodyNEQ("")).Order(ent.Asc(promptappend.FieldCreatedAt), ent.Asc(promptappend.FieldID)).Select(promptappend.FieldID, promptappend.FieldSessionID, promptappend.FieldBody, promptappend.FieldCreatedAt).All(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]agentprofile.PromptAppend, 0, len(rows))
	for _, row := range rows {
		result = append(result, agentprofile.PromptAppend{ID: row.ID, SessionID: row.SessionID, Body: row.Body, CreatedAt: row.CreatedAt})
	}
	return result, nil
}

func (r *AgentProfileRepository) ThreadForPrompt(ctx context.Context, promptID string) (string, error) {
	prompt, err := r.client.PromptAppend.Get(ctx, promptID)
	if err != nil {
		return "", err
	}
	// Prefer the run that received this append, so workflow transitions cannot select a newer thread.
	if prompt.DispatchedProcessRunID != "" {
		run, err := r.client.ProcessRun.Get(ctx, prompt.DispatchedProcessRunID)
		if err == nil && run.CodexSessionID != "" {
			return run.CodexSessionID, nil
		}
		if err != nil && !ent.IsNotFound(err) {
			return "", err
		}
	}
	run, err := r.client.ProcessRun.Query().Where(processrun.SessionIDEQ(prompt.SessionID), processrun.StartedAtLTE(prompt.CreatedAt), processrun.CodexSessionIDNEQ("")).Order(ent.Desc(processrun.FieldStartedAt), ent.Desc(processrun.FieldID)).First(ctx)
	if err == nil {
		return run.CodexSessionID, nil
	}
	if !ent.IsNotFound(err) {
		return "", err
	}
	card, err := r.client.Session.Query().Where(entsession.IDEQ(prompt.SessionID)).Only(ctx)
	if ent.IsNotFound(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return card.CodexSessionID, nil
}
