package question

import (
	"strings"
	"testing"
)

func TestDefaultPolicyAllowsCustomAnswerForEveryQuestion(t *testing.T) {
	request := Request{
		ID:     "request-1",
		Status: RequestPending,
		Questions: []Question{
			{ID: "q1", Options: []Option{{ID: "preset", Label: "Preset"}}},
			{ID: "q2"},
		},
	}
	answers := []Answer{
		{QuestionID: "q1", CustomAnswer: "custom with preset available"},
		{QuestionID: "q2", CustomAnswer: "custom without preset"},
	}

	if err := (DefaultPolicy{}).CanSubmit(request, answers); err != nil {
		t.Fatalf("CanSubmit() error = %v", err)
	}
}

func TestDefaultPolicyRejectsInvalidAnswers(t *testing.T) {
	preset := OptionID("preset")
	tests := []struct {
		name      string
		questions []Question
		answers   []Answer
		wantError string
	}{
		{
			name:      "empty answer",
			questions: []Question{{ID: "q1"}},
			answers:   []Answer{{QuestionID: "q1", CustomAnswer: "   "}},
			wantError: "requires an option or custom answer",
		},
		{
			name:      "preset and custom answer",
			questions: []Question{{ID: "q1", Options: []Option{{ID: preset, Label: "Preset"}}}},
			answers:   []Answer{{QuestionID: "q1", SelectedOptionID: &preset, CustomAnswer: "custom"}},
			wantError: "cannot use option and custom answer together",
		},
		{
			name:      "duplicate answer",
			questions: []Question{{ID: "q1"}, {ID: "q2"}},
			answers: []Answer{
				{QuestionID: "q1", CustomAnswer: "first"},
				{QuestionID: "q1", CustomAnswer: "duplicate"},
			},
			wantError: "duplicate answers",
		},
		{
			name:      "missing answer",
			questions: []Question{{ID: "q1"}, {ID: "q2"}},
			answers:   []Answer{{QuestionID: "q1", CustomAnswer: "only one"}},
			wantError: "all questions must be answered",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := Request{ID: "request-1", Status: RequestPending, Questions: tt.questions}
			err := (DefaultPolicy{}).CanSubmit(request, tt.answers)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("CanSubmit() error = %v, want substring %q", err, tt.wantError)
			}
		})
	}
}
