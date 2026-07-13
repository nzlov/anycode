package question

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/application/apperror"
	domain "github.com/nzlov/anycode/internal/domain/question"
)

func TestSubmitBatchStoresAllAnswers(t *testing.T) {
	repo := newFakeRepository()
	service := New(repo)
	batch := pendingBatch()
	repo.batches[batch.ID] = batch
	optionA := domain.OptionID("a")
	input := SubmitBatchInput{BatchID: batch.ID, Answers: []domain.Answer{
		{QuestionID: "q1", SelectedOptionID: &optionA},
		{QuestionID: "q2", CustomAnswer: "custom"},
	}}

	got, err := service.SubmitBatch(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.BatchAnswered || repo.batches[batch.ID].Status != domain.BatchAnswered {
		t.Fatalf("batch = %#v", got)
	}
	if repo.batches[batch.ID].Questions[1].CustomAnswer != "custom" {
		t.Fatalf("answers = %#v", repo.batches[batch.ID].Questions)
	}
}

func TestSubmitBatchRejectsIncompleteAnswers(t *testing.T) {
	repo := newFakeRepository()
	service := New(repo)
	batch := pendingBatch()
	repo.batches[batch.ID] = batch
	optionA := domain.OptionID("a")

	_, err := service.SubmitBatch(context.Background(), SubmitBatchInput{
		BatchID: batch.ID,
		Answers: []domain.Answer{{QuestionID: "q1", SelectedOptionID: &optionA}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != apperror.CodeValidationFailed {
		t.Fatalf("error = %#v", err)
	}
}

func TestSubmitBatchIsIdempotentForSameAnswers(t *testing.T) {
	repo := newFakeRepository()
	service := New(repo)
	batch := pendingBatch()
	repo.batches[batch.ID] = batch
	optionA := domain.OptionID("a")
	input := SubmitBatchInput{BatchID: batch.ID, Answers: []domain.Answer{
		{QuestionID: "q1", SelectedOptionID: &optionA},
		{QuestionID: "q2", CustomAnswer: "custom"},
	}}
	if _, err := service.SubmitBatch(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SubmitBatch(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if repo.submitCalls != 1 {
		t.Fatalf("submit calls = %d", repo.submitCalls)
	}
}

func TestSubmitBatchRejectsDifferentAnsweredValue(t *testing.T) {
	repo := newFakeRepository()
	service := New(repo)
	batch := pendingBatch()
	repo.batches[batch.ID] = batch
	optionA := domain.OptionID("a")
	if _, err := service.SubmitBatch(context.Background(), SubmitBatchInput{BatchID: batch.ID, Answers: []domain.Answer{
		{QuestionID: "q1", SelectedOptionID: &optionA},
		{QuestionID: "q2", CustomAnswer: "first"},
	}}); err != nil {
		t.Fatal(err)
	}
	_, err := service.SubmitBatch(context.Background(), SubmitBatchInput{BatchID: batch.ID, Answers: []domain.Answer{
		{QuestionID: "q1", SelectedOptionID: &optionA},
		{QuestionID: "q2", CustomAnswer: "different"},
	}})
	if err == nil {
		t.Fatal("expected mismatched answer error")
	}
}

func TestCreateBatchAndGetBatch(t *testing.T) {
	repo := newFakeRepository()
	service := New(repo)
	service.generateID = sequenceIDs("batch-1", "question-1")
	origin := domain.ProcessRunID("process-1")

	created, err := service.CreateBatch(context.Background(), CreateBatchInput{
		SessionID:          "session-1",
		OriginProcessRunID: &origin,
		Questions:          []domain.Question{{Title: "Continue?", Options: []domain.Option{{ID: "yes", Label: "Yes"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "batch-1" || created.Questions[0].ID != "question-1" || created.OriginProcessRunID == nil {
		t.Fatalf("created = %#v", created)
	}
	got, err := service.GetBatch(context.Background(), created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestGetBatchReturnsStructuredNotFound(t *testing.T) {
	service := New(newFakeRepository())
	_, err := service.GetBatch(context.Background(), "missing")
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != apperror.CodeNotFound {
		t.Fatalf("error = %#v", err)
	}
}

func TestListPendingBySessionFiltersAnsweredBatches(t *testing.T) {
	repo := newFakeRepository()
	pending := pendingBatch()
	answered := pendingBatch()
	answered.ID = "batch-answered"
	answered.Status = domain.BatchAnswered
	repo.batches[pending.ID] = pending
	repo.batches[answered.ID] = answered
	service := New(repo)
	got, err := service.ListPendingBySession(context.Background(), pending.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != pending.ID {
		t.Fatalf("pending = %#v", got)
	}
}

func TestQuestionBatchUpdatesPublishesLiveChanges(t *testing.T) {
	repo := newFakeRepository()
	service := New(repo)
	service.generateID = sequenceIDs("batch-1", "question-1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates, err := service.QuestionBatchUpdates(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateBatch(context.Background(), CreateBatchInput{
		SessionID: "session-1",
		Questions: []domain.Question{{Title: "Continue?", Options: []domain.Option{{ID: "yes", Label: "Yes"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-updates:
		if got.ID != created.ID {
			t.Fatalf("update = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("question update not published")
	}
}

func TestQuestionBatchUpdatesDoesNotDropBurst(t *testing.T) {
	service := New(newFakeRepository())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates, err := service.QuestionBatchUpdates(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	const count = 32
	for i := 0; i < count; i++ {
		service.PublishBatch(BatchDTO{ID: domain.BatchID(string(rune('a' + i))), SessionID: "session-1", Status: domain.BatchPending})
	}
	for i := 0; i < count; i++ {
		select {
		case <-updates:
		case <-time.After(time.Second):
			t.Fatalf("received %d of %d updates", i, count)
		}
	}
}

func TestQuestionBatchUpdatesCloseWithContext(t *testing.T) {
	service := New(newFakeRepository())
	ctx, cancel := context.WithCancel(context.Background())
	updates, err := service.QuestionBatchUpdates(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case _, ok := <-updates:
		if ok {
			t.Fatal("updates channel remained open")
		}
	case <-time.After(time.Second):
		t.Fatal("updates channel did not close")
	}
}

func TestCancelPendingBySessionPublishesCancellation(t *testing.T) {
	repo := newFakeRepository()
	service := New(repo)
	batch := pendingBatch()
	repo.batches[batch.ID] = batch
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates, err := service.QuestionBatchUpdates(ctx, batch.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.CancelPendingBySession(context.Background(), batch.SessionID, "session stopped"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-updates:
		if got.Status != domain.BatchCancelled {
			t.Fatalf("update = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation update not published")
	}
}

func pendingBatch() domain.Batch {
	return domain.Batch{
		ID:             "batch-1",
		SessionID:      "session-1",
		Status:         domain.BatchPending,
		DeliveryStatus: domain.DeliveryNone,
		Questions: []domain.Question{
			{ID: "q1", Options: []domain.Option{{ID: "a", Label: "A"}}, AllowCustom: false},
			{ID: "q2", AllowCustom: true},
		},
	}
}

type fakeRepository struct {
	mu          sync.Mutex
	batches     map[domain.BatchID]domain.Batch
	submitCalls int
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{batches: map[domain.BatchID]domain.Batch{}}
}

func (r *fakeRepository) CreateBatch(_ context.Context, batch domain.Batch) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.batches[batch.ID] = batch
	return nil
}

func (r *fakeRepository) FindBatch(_ context.Context, id domain.BatchID) (domain.Batch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	batch, ok := r.batches[id]
	if !ok {
		return domain.Batch{}, errors.New("not found")
	}
	return batch, nil
}

func (r *fakeRepository) ListPendingBySession(_ context.Context, sessionID domain.SessionID) ([]domain.Batch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []domain.Batch
	for _, batch := range r.batches {
		if batch.SessionID == sessionID && batch.Status == domain.BatchPending {
			result = append(result, batch)
		}
	}
	return result, nil
}

func (r *fakeRepository) SubmitAnswers(_ context.Context, id domain.BatchID, answers []domain.Answer) (domain.Batch, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	batch, ok := r.batches[id]
	if !ok {
		return domain.Batch{}, false, errors.New("not found")
	}
	if batch.Status != domain.BatchPending {
		return batch, false, nil
	}
	r.submitCalls++
	byQuestion := map[domain.QuestionID]domain.Answer{}
	for _, answer := range answers {
		byQuestion[answer.QuestionID] = answer
	}
	for i := range batch.Questions {
		answer := byQuestion[batch.Questions[i].ID]
		batch.Questions[i].SelectedOptionID = answer.SelectedOptionID
		batch.Questions[i].CustomAnswer = answer.CustomAnswer
		batch.Questions[i].Answer = answer.Payload
		batch.Questions[i].Status = "answered"
	}
	batch.Status = domain.BatchAnswered
	r.batches[id] = batch
	return batch, true, nil
}

func (r *fakeRepository) CancelPendingBySession(_ context.Context, sessionID domain.SessionID, _ string) ([]domain.Batch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []domain.Batch
	for id, batch := range r.batches {
		if batch.SessionID != sessionID || batch.Status != domain.BatchPending {
			continue
		}
		batch.Status = domain.BatchCancelled
		r.batches[id] = batch
		result = append(result, batch)
	}
	return result, nil
}

func sequenceIDs(values ...string) func() (string, error) {
	index := 0
	return func() (string, error) {
		if index >= len(values) {
			return "", errors.New("no ids left")
		}
		value := values[index]
		index++
		return value, nil
	}
}
