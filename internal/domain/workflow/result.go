package workflow

import (
	"errors"
	"fmt"
	"strings"
)

const ResultVersion = 1

type ResultOutcome string

const (
	ResultSuccess ResultOutcome = "success"
	ResultPartial ResultOutcome = "partial"
	ResultFailure ResultOutcome = "failure"
)

type Result struct {
	Version   int              `json:"version"`
	Outcome   ResultOutcome    `json:"outcome"`
	Summary   string           `json:"summary"`
	Data      map[string]any   `json:"data"`
	Checks    []ResultCheck    `json:"checks"`
	Warnings  []ResultWarning  `json:"warnings"`
	Artifacts []ResultArtifact `json:"artifacts"`
}

type ResultCheck struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Source string `json:"source"`
}

type ResultWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ResultArtifact struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Ref   string `json:"ref"`
}

func (r *Result) Normalize() {
	if r == nil {
		return
	}
	if r.Data == nil {
		r.Data = map[string]any{}
	}
	if r.Checks == nil {
		r.Checks = []ResultCheck{}
	}
	if r.Warnings == nil {
		r.Warnings = []ResultWarning{}
	}
	if r.Artifacts == nil {
		r.Artifacts = []ResultArtifact{}
	}
}

func (r Result) Validate() error {
	if r.Version != ResultVersion {
		return fmt.Errorf("workflow result version must be %d", ResultVersion)
	}
	switch r.Outcome {
	case ResultSuccess, ResultPartial, ResultFailure:
	default:
		return fmt.Errorf("unsupported workflow result outcome %q", r.Outcome)
	}
	if strings.TrimSpace(r.Summary) == "" {
		return errors.New("workflow result summary is required")
	}
	if r.Data == nil {
		return errors.New("workflow result data is required")
	}
	for index, check := range r.Checks {
		if strings.TrimSpace(check.ID) == "" || strings.TrimSpace(check.Label) == "" {
			return fmt.Errorf("workflow result check %d requires id and label", index)
		}
		switch check.Status {
		case "passed", "warning", "failed":
		default:
			return fmt.Errorf("workflow result check %q has unsupported status %q", check.ID, check.Status)
		}
		switch check.Source {
		case "agent", "system":
		default:
			return fmt.Errorf("workflow result check %q has unsupported source %q", check.ID, check.Source)
		}
	}
	for index, warning := range r.Warnings {
		if strings.TrimSpace(warning.Code) == "" || strings.TrimSpace(warning.Message) == "" {
			return fmt.Errorf("workflow result warning %d requires code and message", index)
		}
	}
	for index, artifact := range r.Artifacts {
		if strings.TrimSpace(artifact.Kind) == "" || strings.TrimSpace(artifact.Label) == "" || strings.TrimSpace(artifact.Ref) == "" {
			return fmt.Errorf("workflow result artifact %d requires kind, label, and ref", index)
		}
	}
	return nil
}
