package workflow

import "testing"

func TestResultValidate(t *testing.T) {
	valid := Result{Version: ResultVersion, Outcome: ResultSuccess, Summary: "done", Data: map[string]any{}}
	valid.Normalize()
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	tests := []Result{
		{Version: 2, Outcome: ResultSuccess, Summary: "done", Data: map[string]any{}},
		{Version: ResultVersion, Outcome: "needs_input", Summary: "question", Data: map[string]any{}},
		{Version: ResultVersion, Outcome: ResultSuccess, Data: map[string]any{}},
		{Version: ResultVersion, Outcome: ResultSuccess, Summary: "done", Data: map[string]any{}, Checks: []ResultCheck{{ID: "check", Label: "Check", Status: "pending", Source: "agent"}}},
	}
	for _, result := range tests {
		if err := result.Validate(); err == nil {
			t.Fatalf("Validate() accepted %#v", result)
		}
	}
}
