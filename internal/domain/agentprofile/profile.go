package agentprofile

import (
	"context"
	"time"
)

// Configuration and Analysis are the canonical global profile, independent of session history.
type Configuration struct {
	Enabled         bool
	Cron            string
	Model           string
	ReasoningEffort string
}

type Analysis struct {
	Prompt       string
	Through      *time.Time
	StartedAt    *time.Time
	FinishedAt   *time.Time
	Status       string
	Error        string
	SessionCount int
	MessageCount int
	Progress     AnalysisProgress
}

type AnalysisProgress struct {
	Stage             string
	Percent           int
	CompletedMessages int
	TotalMessages     int
}

type Profile struct {
	Configuration Configuration
	Analysis      Analysis
}

func Default() Profile {
	return Profile{Configuration: Configuration{Enabled: true}, Analysis: Analysis{Status: "idle"}}
}

type PromptAppend struct {
	ID        string
	SessionID string
	Body      string
	CreatedAt time.Time
}

type AnalysisResult struct {
	Prompt           string   `json:"prompt"`
	ContextPromptIDs []string `json:"contextPromptIds"`
}

type Repository interface {
	Get(context.Context) (Profile, error)
	SaveConfiguration(context.Context, Configuration) error
	SaveAnalysis(context.Context, Analysis) error
	ListPromptAppends(ctx context.Context, since, until time.Time) ([]PromptAppend, error)
	ThreadForPrompt(ctx context.Context, promptID string) (string, error)
}

type Analyzer interface {
	AnalyzeProfile(ctx context.Context, previous, history, model, reasoningEffort string) (AnalysisResult, error)
}
