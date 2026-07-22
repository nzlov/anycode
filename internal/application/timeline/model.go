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

func timelineOrderKey(createdAt time.Time, sequence int64, id string) string {
	return fmt.Sprintf("%020d:%020d:%s", createdAt.UnixNano(), sequence, id)
}
