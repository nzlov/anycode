package statistics

import (
	"context"
	"time"
)

type Metrics struct {
	CreatedCards int
	ClosedCards  int
	FilesChanged int
	TotalTokens  int64
}

type ProjectMetrics struct {
	Key     string
	Label   string
	Metrics Metrics
}

type TimelineBucket struct {
	Key      string
	Label    string
	Projects []ProjectMetrics
}

type Dashboard struct {
	Today Metrics
	Total Metrics
	ByDay []TimelineBucket
}

type DateRange struct {
	StartDay string
	EndDay   string
}

type DailyUpdate struct {
	SessionID    string
	ProjectID    string
	OccurredAt   time.Time
	Created      bool
	Closed       bool
	TokenDelta   int64
	FilesChanged *int
}

type Recorder interface {
	RecordDaily(ctx context.Context, update DailyUpdate) error
}

type Repository interface {
	Recorder
	Dashboard(ctx context.Context, today string, dateRange DateRange) (Dashboard, error)
}
