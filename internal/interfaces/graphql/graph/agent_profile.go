package graph

import (
	agentprofileapp "github.com/nzlov/anycode/internal/application/agentprofile"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph/model"
)

func toAgentProfile(dto agentprofileapp.DTO) *model.AgentProfile {
	return &model.AgentProfile{
		Enabled: dto.Configuration.Enabled, Cron: dto.Configuration.Cron, Model: dto.Configuration.Model,
		ReasoningEffort: dto.Configuration.ReasoningEffort,
		Prompt:          dto.Analysis.Prompt, Status: dto.Analysis.Status, Error: dto.Analysis.Error,
		Through: dto.Analysis.Through, StartedAt: dto.Analysis.StartedAt, FinishedAt: dto.Analysis.FinishedAt,
		SessionCount: dto.Analysis.SessionCount, MessageCount: dto.Analysis.MessageCount,
		Progress: &model.AgentProfileProgress{
			Stage: dto.Analysis.Progress.Stage, Percent: dto.Analysis.Progress.Percent,
			CompletedMessages: dto.Analysis.Progress.CompletedMessages, TotalMessages: dto.Analysis.Progress.TotalMessages,
		},
		Schedule: toProfileSchedule(dto.Schedule),
	}
}

func toProfileSchedule(preview agentprofileapp.SchedulePreview) *model.AgentProfileSchedule {
	return &model.AgentProfileSchedule{Valid: preview.Valid, Description: preview.Description, Error: preview.Error, Timezone: preview.Timezone, NextRunAt: preview.NextRunAt}
}
