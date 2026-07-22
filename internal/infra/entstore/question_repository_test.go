package entstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/domain/question"
)

func TestQuestionRepositoryCreatesFindsSubmitsAndCancels(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{
		DatabaseURL: filepath.Join(t.TempDir(), "anycode.db"),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate store: %v", err)
	}

	repo := store.Questions()
	createdAt := time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC)
	optionID := question.OptionID("option-1")
	originProcessRunID := question.ProcessRunID("process-run-1")
	request := question.Request{
		ID:                 question.RequestID("request-1"),
		SessionID:          question.SessionID("session-1"),
		OriginProcessRunID: &originProcessRunID,
		Status:             question.RequestPending,
		CreatedAt:          createdAt,
		Questions: []question.Question{
			{
				ID:        question.QuestionID("question-1"),
				RequestID: question.RequestID("request-1"),
				Body:      "Which path should the workflow take?",
				Type:      "choice",
				Status:    string(question.RequestPending),
				Options: []question.Option{
					{
						ID:          optionID,
						Label:       "Continue",
						Description: "Proceed with the current plan",
						Payload: map[string]any{
							"next": "continue",
						},
					},
				},
			},
		},
	}
	if err := repo.CreateRequest(ctx, request); err != nil {
		t.Fatalf("create request: %v", err)
	}

	found, err := repo.FindRequest(ctx, request.ID)
	if err != nil {
		t.Fatalf("find request: %v", err)
	}
	if found.ID != request.ID || found.SessionID != request.SessionID || found.Status != question.RequestPending || !found.CreatedAt.Equal(createdAt) {
		t.Fatalf("found request mismatch: %#v", found)
	}
	if found.OriginProcessRunID == nil || *found.OriginProcessRunID != originProcessRunID {
		t.Fatalf("origin process run mismatch: %#v", found)
	}
	if len(found.Questions) != 1 || found.Questions[0].ID != "question-1" || len(found.Questions[0].Options) != 1 {
		t.Fatalf("found questions mismatch: %#v", found.Questions)
	}
	if found.Questions[0].Options[0].Payload["next"] != "continue" {
		t.Fatalf("found option payload mismatch: %#v", found.Questions[0].Options[0].Payload)
	}
	pending, err := repo.ListPendingRequestsBySession(ctx, request.SessionID)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != request.ID {
		t.Fatalf("pending requests = %#v", pending)
	}
	latest, ok, err := repo.FindLatestRequestBySession(ctx, request.SessionID)
	if err != nil || !ok || latest.ID != request.ID {
		t.Fatalf("latest request = %#v, %t, %v", latest, ok, err)
	}
	byOrigin, ok, err := repo.FindPendingRequestByOriginProcessRun(ctx, originProcessRunID)
	if err != nil || !ok || byOrigin.ID != request.ID {
		t.Fatalf("pending request by process = %#v, %t, %v", byOrigin, ok, err)
	}

	persisted, transitioned, err := repo.SubmitAnswers(ctx, request.ID, []question.Answer{
		{
			QuestionID:       question.QuestionID("question-1"),
			SelectedOptionID: &optionID,
			Payload: map[string]any{
				"accepted": true,
			},
		},
	})
	if err != nil {
		t.Fatalf("submit answers: %v", err)
	}
	if !transitioned || persisted.Status != question.RequestAnswered {
		t.Fatalf("submit transition = %#v %t", persisted, transitioned)
	}
	answered, err := repo.FindRequest(ctx, request.ID)
	if err != nil {
		t.Fatalf("find answered request: %v", err)
	}
	if answered.Status != question.RequestAnswered || answered.AnsweredAt == nil {
		t.Fatalf("answered request status mismatch: %#v", answered)
	}
	if len(answered.Questions) != 1 {
		t.Fatalf("answered questions mismatch: %#v", answered.Questions)
	}
	answer := answered.Questions[0]
	if answer.SelectedOptionID == nil || *answer.SelectedOptionID != optionID || answer.CustomAnswer != "" || answer.Answer["accepted"] != true {
		t.Fatalf("answered question mismatch: %#v", answer)
	}

	cancelRequest := question.Request{
		ID:        question.RequestID("request-2"),
		SessionID: request.SessionID,
		Status:    question.RequestPending,
		Questions: []question.Question{
			{
				ID:        question.QuestionID("question-2"),
				RequestID: question.RequestID("request-2"),
				Body:      "Pending",
				Type:      "choice",
				Status:    string(question.RequestPending),
			},
		},
	}
	if err := repo.CreateRequest(ctx, cancelRequest); err != nil {
		t.Fatalf("create cancel request: %v", err)
	}
	pending, err = repo.ListPendingRequestsBySession(ctx, request.SessionID)
	if err != nil {
		t.Fatalf("list pending after creating cancel request: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != cancelRequest.ID {
		t.Fatalf("pending after answer = %#v", pending)
	}
	otherSessionRequest := question.Request{
		ID:        question.RequestID("request-3"),
		SessionID: question.SessionID("session-2"),
		Status:    question.RequestPending,
	}
	if err := repo.CreateRequest(ctx, otherSessionRequest); err != nil {
		t.Fatalf("create other session request: %v", err)
	}

	cancelledRequests, err := repo.CancelPendingRequestsBySession(ctx, request.SessionID, "session stopped")
	if err != nil {
		t.Fatalf("cancel pending by session: %v", err)
	}
	if len(cancelledRequests) != 1 || cancelledRequests[0].ID != cancelRequest.ID {
		t.Fatalf("cancelled requests = %#v", cancelledRequests)
	}
	cancelled, err := repo.FindRequest(ctx, cancelRequest.ID)
	if err != nil {
		t.Fatalf("find cancelled request: %v", err)
	}
	if cancelled.Status != question.RequestCancelled {
		t.Fatalf("cancelled request status mismatch: %#v", cancelled)
	}
	lateSubmit, transitioned, err := repo.SubmitAnswers(ctx, cancelRequest.ID, []question.Answer{
		{QuestionID: "question-2", CustomAnswer: "late answer"},
	})
	if err != nil {
		t.Fatalf("submit cancelled request: %v", err)
	}
	if transitioned || lateSubmit.Status != question.RequestCancelled {
		t.Fatalf("cancelled request was revived: %#v transitioned=%t", lateSubmit, transitioned)
	}
	stillPending, err := repo.FindRequest(ctx, otherSessionRequest.ID)
	if err != nil {
		t.Fatalf("find other session request: %v", err)
	}
	if stillPending.Status != question.RequestPending {
		t.Fatalf("other session request should remain pending: %#v", stillPending)
	}
	answeredAgain, err := repo.FindRequest(ctx, request.ID)
	if err != nil {
		t.Fatalf("find answered after cancel: %v", err)
	}
	if answeredAgain.Status != question.RequestAnswered {
		t.Fatalf("answered request should not be cancelled: %#v", answeredAgain)
	}
}
