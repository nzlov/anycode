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
	workflowRunID := question.WorkflowRunID("workflow-run-1")
	batch := question.Batch{
		ID:            question.BatchID("batch-1"),
		SessionID:     question.SessionID("session-1"),
		WorkflowRunID: &workflowRunID,
		Status:        question.BatchPending,
		CreatedAt:     createdAt,
		Questions: []question.Question{
			{
				ID:          question.QuestionID("question-1"),
				BatchID:     question.BatchID("batch-1"),
				Title:       "Choose path",
				Body:        "Which path should the workflow take?",
				Type:        "choice",
				AllowCustom: true,
				Status:      string(question.BatchPending),
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
	if err := repo.CreateBatch(ctx, batch); err != nil {
		t.Fatalf("create batch: %v", err)
	}

	found, err := repo.FindBatch(ctx, batch.ID)
	if err != nil {
		t.Fatalf("find batch: %v", err)
	}
	if found.ID != batch.ID || found.SessionID != batch.SessionID || found.Status != question.BatchPending || !found.CreatedAt.Equal(createdAt) {
		t.Fatalf("found batch mismatch: %#v", found)
	}
	if found.Delivery != question.DeliveryPending {
		t.Fatalf("found delivery = %q", found.Delivery)
	}
	if found.WorkflowRunID == nil || *found.WorkflowRunID != workflowRunID {
		t.Fatalf("workflow run id mismatch: %#v", found.WorkflowRunID)
	}
	if len(found.Questions) != 1 || found.Questions[0].ID != "question-1" || len(found.Questions[0].Options) != 1 {
		t.Fatalf("found questions mismatch: %#v", found.Questions)
	}
	if found.Questions[0].Options[0].Payload["next"] != "continue" {
		t.Fatalf("found option payload mismatch: %#v", found.Questions[0].Options[0].Payload)
	}
	pending, err := repo.ListPendingBySession(ctx, batch.SessionID)
	if err != nil {
		t.Fatalf("list pending: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != batch.ID {
		t.Fatalf("pending batches = %#v", pending)
	}
	updatedDelivery, changed, err := repo.SetDeliveryStatus(ctx, batch.ID, question.DeliveryRecoveryRequired)
	if err != nil || !changed || updatedDelivery.Delivery != question.DeliveryRecoveryRequired {
		t.Fatalf("set recovery required = %#v changed=%v error=%v", updatedDelivery, changed, err)
	}
	latest, ok, err := repo.FindLatestBySession(ctx, batch.SessionID)
	if err != nil || !ok || latest.ID != batch.ID || latest.Delivery != question.DeliveryRecoveryRequired {
		t.Fatalf("latest recovery batch = %#v ok=%v error=%v", latest, ok, err)
	}

	persisted, transitioned, err := repo.SubmitAnswers(ctx, batch.ID, []question.Answer{
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
	if !transitioned || persisted.Status != question.BatchAnswered || persisted.Delivery != question.DeliveryRecoveryRequired {
		t.Fatalf("submit transition = %#v %t", persisted, transitioned)
	}
	if delivered, changed, err := repo.SetDeliveryStatus(ctx, batch.ID, question.DeliveryDelivered); err != nil || !changed || delivered.Delivery != question.DeliveryDelivered {
		t.Fatalf("set delivered = %#v changed=%v error=%v", delivered, changed, err)
	}

	answered, err := repo.FindBatch(ctx, batch.ID)
	if err != nil {
		t.Fatalf("find answered batch: %v", err)
	}
	if answered.Status != question.BatchAnswered || answered.AnsweredAt == nil {
		t.Fatalf("answered batch status mismatch: %#v", answered)
	}
	if len(answered.Questions) != 1 {
		t.Fatalf("answered questions mismatch: %#v", answered.Questions)
	}
	answer := answered.Questions[0]
	if answer.SelectedOptionID == nil || *answer.SelectedOptionID != optionID || answer.CustomAnswer != "" || answer.Answer["accepted"] != true {
		t.Fatalf("answered question mismatch: %#v", answer)
	}

	cancelBatch := question.Batch{
		ID:        question.BatchID("batch-2"),
		SessionID: batch.SessionID,
		Status:    question.BatchPending,
		Questions: []question.Question{
			{
				ID:      question.QuestionID("question-2"),
				BatchID: question.BatchID("batch-2"),
				Title:   "Pending",
				Type:    "choice",
				Status:  string(question.BatchPending),
			},
		},
	}
	if err := repo.CreateBatch(ctx, cancelBatch); err != nil {
		t.Fatalf("create cancel batch: %v", err)
	}
	pending, err = repo.ListPendingBySession(ctx, batch.SessionID)
	if err != nil {
		t.Fatalf("list pending after creating cancel batch: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != cancelBatch.ID {
		t.Fatalf("pending after answer = %#v", pending)
	}
	otherSessionBatch := question.Batch{
		ID:        question.BatchID("batch-3"),
		SessionID: question.SessionID("session-2"),
		Status:    question.BatchPending,
	}
	if err := repo.CreateBatch(ctx, otherSessionBatch); err != nil {
		t.Fatalf("create other session batch: %v", err)
	}

	cancelledBatches, err := repo.CancelPendingBySession(ctx, batch.SessionID, "session stopped")
	if err != nil {
		t.Fatalf("cancel pending by session: %v", err)
	}
	if len(cancelledBatches) != 1 || cancelledBatches[0].ID != cancelBatch.ID {
		t.Fatalf("cancelled batches = %#v", cancelledBatches)
	}
	cancelled, err := repo.FindBatch(ctx, cancelBatch.ID)
	if err != nil {
		t.Fatalf("find cancelled batch: %v", err)
	}
	if cancelled.Status != question.BatchCancelled {
		t.Fatalf("cancelled batch status mismatch: %#v", cancelled)
	}
	lateSubmit, transitioned, err := repo.SubmitAnswers(ctx, cancelBatch.ID, []question.Answer{
		{QuestionID: "question-2", CustomAnswer: "late answer"},
	})
	if err != nil {
		t.Fatalf("submit cancelled batch: %v", err)
	}
	if transitioned || lateSubmit.Status != question.BatchCancelled {
		t.Fatalf("cancelled batch was revived: %#v transitioned=%t", lateSubmit, transitioned)
	}
	stillPending, err := repo.FindBatch(ctx, otherSessionBatch.ID)
	if err != nil {
		t.Fatalf("find other session batch: %v", err)
	}
	if stillPending.Status != question.BatchPending {
		t.Fatalf("other session batch should remain pending: %#v", stillPending)
	}
	answeredAgain, err := repo.FindBatch(ctx, batch.ID)
	if err != nil {
		t.Fatalf("find answered after cancel: %v", err)
	}
	if answeredAgain.Status != question.BatchAnswered {
		t.Fatalf("answered batch should not be cancelled: %#v", answeredAgain)
	}
}
