package timeline

import (
	"fmt"
	"time"

	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
)

type DTO struct {
	ID            eventdomain.ID
	Type          processdomain.CodexEventType
	OrderKey      string
	CorrelationID string
	Phase         processdomain.CodexPhase
	Content       processdomain.CodexEventContent
	OccurredAt    string
	Usage         *TokenUsageDTO
	Causality     eventdomain.Causality
	Group         *GroupDTO
}

type GroupDTO struct {
	Kind    string
	Label   string
	Members []DTO
}

type TokenUsageDTO struct {
	InputTokens                  int
	CachedInputTokens            int
	OutputTokens                 int
	ReasoningOutputTokens        int
	TotalTokens                  int
	ContextWindow                int
	CurrentInputTokens           int
	CurrentCachedInputTokens     int
	CurrentOutputTokens          int
	CurrentReasoningOutputTokens int
	CurrentTotalTokens           int
	CompactionCount              int
}

type Page struct {
	Items        []DTO
	Page         int
	PageSize     int
	Total        int
	NextCursor   string
	Usage        *TokenUsageDTO
	ProcessUsage []UsageAttributionDTO
	NodeUsage    []UsageAttributionDTO
}

type UsageAttributionDTO struct {
	ProcessRunID string
	NodeRunID    string
	Usage        TokenUsageDTO
}

func timelineOrderKey(createdAt time.Time, sourceGroup int, sourceOffset int64, sourceIndex int, id string) string {
	return fmt.Sprintf("%020d:%06d:%020d:%06d:%s", createdAt.UnixNano(), sourceGroup, sourceOffset, sourceIndex, id)
}

func usageDTO(content processdomain.CodexUsageContent) *TokenUsageDTO {
	return &TokenUsageDTO{
		InputTokens:                  content.InputTokens,
		CachedInputTokens:            content.CachedInputTokens,
		OutputTokens:                 content.OutputTokens,
		ReasoningOutputTokens:        content.ReasoningOutputTokens,
		TotalTokens:                  content.TotalTokens,
		ContextWindow:                content.ContextWindow,
		CurrentInputTokens:           content.CurrentInputTokens,
		CurrentCachedInputTokens:     content.CurrentCachedInputTokens,
		CurrentOutputTokens:          content.CurrentOutputTokens,
		CurrentReasoningOutputTokens: content.CurrentReasoningOutputTokens,
		CurrentTotalTokens:           content.CurrentTotalTokens,
	}
}
