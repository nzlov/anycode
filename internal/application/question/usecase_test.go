package question

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/application/apperror"
	domain "github.com/nzlov/anycode/internal/domain/question"
)

func TestSubmitBatchStoresAllAnswersAndResumesWaiter(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository(batchFixture())
	waiter := newRecordingWaiter()
	service := New(repo, waiter)

	optionID := domain.OptionID("yes")
	got, err := service.SubmitBatch(ctx, SubmitBatchInput{
		BatchID: "batch-1",
		Answers: []domain.Answer{
			{QuestionID: "q1", SelectedOptionID: &optionID},
			{QuestionID: "q2", CustomAnswer: "ship it"},
		},
	})
	if err != nil {
		t.Fatalf("SubmitBatch() error = %v", err)
	}
	if got.Status != domain.BatchAnswered {
		t.Fatalf("Status = %q", got.Status)
	}
	if repo.batches["batch-1"].Status != domain.BatchAnswered {
		t.Fatalf("stored batch status = %q", repo.batches["batch-1"].Status)
	}
	if waiter.resumedBatchID != "batch-1" || len(waiter.resumedAnswers) != 2 {
		t.Fatalf("resumed waiter = %q %#v", waiter.resumedBatchID, waiter.resumedAnswers)
	}
}

func TestSubmitBatchRejectsMissingAnswer(t *testing.T) {
	repo := newFakeRepository(batchFixture())
	service := New(repo, newRecordingWaiter())
	optionID := domain.OptionID("yes")

	_, err := service.SubmitBatch(context.Background(), SubmitBatchInput{
		BatchID: "batch-1",
		Answers: []domain.Answer{
			{QuestionID: "q1", SelectedOptionID: &optionID},
		},
	})
	if err == nil {
		t.Fatal("SubmitBatch() expected missing answer error")
	}
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != apperror.CodeValidationFailed || appErr.Category != apperror.CategoryValidationError {
		t.Fatalf("SubmitBatch() error = %#v", err)
	}
	if repo.submitCalls != 0 {
		t.Fatalf("SubmitAnswers calls = %d", repo.submitCalls)
	}
}

func TestSubmitBatchAcceptsCustomAnswer(t *testing.T) {
	repo := newFakeRepository(batchFixture())
	service := New(repo, nil)
	optionID := domain.OptionID("yes")

	got, err := service.SubmitBatch(context.Background(), SubmitBatchInput{
		BatchID: "batch-1",
		Answers: []domain.Answer{
			{QuestionID: "q1", SelectedOptionID: &optionID},
			{QuestionID: "q2", CustomAnswer: "custom path"},
		},
	})
	if err != nil {
		t.Fatalf("SubmitBatch() error = %v", err)
	}
	if got.Questions[1].CustomAnswer != "custom path" || got.Questions[1].Status != "answered" {
		t.Fatalf("custom question = %#v", got.Questions[1])
	}
}

func TestSubmitBatchIsIdempotentWhenAlreadyAnswered(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository(batchFixture())
	waiter := newRecordingWaiter()
	service := New(repo, waiter)
	optionID := domain.OptionID("yes")
	input := SubmitBatchInput{
		BatchID: "batch-1",
		Answers: []domain.Answer{
			{QuestionID: "q1", SelectedOptionID: &optionID, Payload: map[string]any{"choice": "yes"}},
			{QuestionID: "q2", CustomAnswer: "ship it", Payload: map[string]any{"custom": true}},
		},
	}

	if _, err := service.SubmitBatch(ctx, input); err != nil {
		t.Fatalf("first SubmitBatch() error = %v", err)
	}
	got, err := service.SubmitBatch(ctx, input)
	if err != nil {
		t.Fatalf("second SubmitBatch() error = %v", err)
	}
	if got.Status != domain.BatchAnswered {
		t.Fatalf("Status = %q", got.Status)
	}
	if repo.submitCalls != 1 {
		t.Fatalf("SubmitAnswers calls = %d, want 1", repo.submitCalls)
	}
	if waiter.resumeCalls != 1 {
		t.Fatalf("Resume calls = %d, want 1", waiter.resumeCalls)
	}
}

func TestSubmitBatchRejectsDifferentAnswerWhenAlreadyAnswered(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository(batchFixture())
	service := New(repo, nil)
	optionID := domain.OptionID("yes")
	otherOptionID := domain.OptionID("no")
	if _, err := service.SubmitBatch(ctx, SubmitBatchInput{
		BatchID: "batch-1",
		Answers: []domain.Answer{
			{QuestionID: "q1", SelectedOptionID: &optionID},
			{QuestionID: "q2", CustomAnswer: "ship it"},
		},
	}); err != nil {
		t.Fatalf("first SubmitBatch() error = %v", err)
	}

	_, err := service.SubmitBatch(ctx, SubmitBatchInput{
		BatchID: "batch-1",
		Answers: []domain.Answer{
			{QuestionID: "q1", SelectedOptionID: &otherOptionID},
			{QuestionID: "q2", CustomAnswer: "ship it"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "already answered with different answers") {
		t.Fatalf("second SubmitBatch() error = %v", err)
	}
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != apperror.CodeValidationFailed {
		t.Fatalf("second SubmitBatch() app error = %#v", err)
	}
	if repo.submitCalls != 1 {
		t.Fatalf("SubmitAnswers calls = %d, want 1", repo.submitCalls)
	}
}

func TestCreateBatchAndGetBatch(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository()
	service := New(repo, nil)
	service.now = func() time.Time { return time.Unix(20, 0).UTC() }
	ids := []string{"batch-new", "q-new"}
	service.generateID = func() (string, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	created, err := service.CreateBatch(ctx, CreateBatchInput{
		SessionID: "session-1",
		Questions: []domain.Question{
			{Title: "Approve?", Options: []domain.Option{{ID: "ok", Label: "OK"}}},
		},
	})
	if err != nil {
		t.Fatalf("CreateBatch() error = %v", err)
	}
	if created.ID != "batch-new" || created.Questions[0].ID != "q-new" || created.Questions[0].BatchID != "batch-new" {
		t.Fatalf("created batch = %#v", created)
	}
	got, err := service.GetBatch(ctx, "batch-new")
	if err != nil {
		t.Fatalf("GetBatch() error = %v", err)
	}
	if !reflect.DeepEqual(got, created) {
		t.Fatalf("GetBatch() = %#v, want %#v", got, created)
	}
}

func TestGetBatchReturnsStructuredNotFound(t *testing.T) {
	repo := newFakeRepository()
	service := New(repo, nil)

	_, err := service.GetBatch(context.Background(), "missing")
	if err == nil {
		t.Fatal("GetBatch() expected error")
	}
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != apperror.CodeNotFound || appErr.Details["batchId"] != "missing" {
		t.Fatalf("GetBatch() app error = %#v", err)
	}
}

func TestListPendingBySession(t *testing.T) {
	pending := batchFixture()
	answered := batchFixture()
	answered.ID = "batch-answered"
	answered.Status = domain.BatchAnswered
	other := batchFixture()
	other.ID = "batch-other"
	other.SessionID = "session-2"
	repo := newFakeRepository(pending, answered, other)
	service := New(repo, nil)

	got, err := service.ListPendingBySession(context.Background(), "session-1")
	if err != nil {
		t.Fatalf("ListPendingBySession() error = %v", err)
	}
	if len(got) != 1 || got[0].ID != pending.ID {
		t.Fatalf("ListPendingBySession() = %#v", got)
	}
}

func TestPendingQuestionBatchesSendsExistingAndCreatedBatches(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	existing := batchFixture()
	repo := newFakeRepository(existing)
	service := New(repo, nil)
	service.generateID = func() (string, error) { return "batch-created", nil }

	source, err := service.PendingQuestionBatches(ctx, "session-1")
	if err != nil {
		t.Fatalf("PendingQuestionBatches() error = %v", err)
	}
	if got := <-source; got.ID != existing.ID {
		t.Fatalf("first pending batch = %#v", got)
	}
	created, err := service.CreateBatch(context.Background(), CreateBatchInput{
		SessionID: "session-1",
		Questions: []domain.Question{
			{ID: "question-created", Title: "Approve?"},
		},
	})
	if err != nil {
		t.Fatalf("CreateBatch() error = %v", err)
	}
	select {
	case got := <-source:
		if got.ID != created.ID {
			t.Fatalf("published batch = %#v, want %#v", got, created)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for pending question batch")
	}
}

func TestCancelPendingBySessionCancelsRepositoryAndWaiters(t *testing.T) {
	pending := batchFixture()
	other := batchFixture()
	other.ID = "batch-other"
	other.SessionID = "session-2"
	repo := newFakeRepository(pending, other)
	waiter := newRecordingWaiter()
	service := New(repo, waiter)

	if err := service.CancelPendingBySession(context.Background(), "session-1", "session stopped"); err != nil {
		t.Fatalf("CancelPendingBySession() error = %v", err)
	}
	if got := repo.batches[pending.ID].Status; got != domain.BatchCancelled {
		t.Fatalf("cancelled batch status = %q", got)
	}
	if got := repo.batches[other.ID].Status; got != domain.BatchPending {
		t.Fatalf("other batch status = %q", got)
	}
	if waiter.cancelledBatchID != pending.ID || waiter.cancelReason != "session stopped" {
		t.Fatalf("cancelled waiter = %q %q", waiter.cancelledBatchID, waiter.cancelReason)
	}
}

func TestMemoryAnswerWaiterWaitResume(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	waiter := NewMemoryAnswerWaiter()
	optionID := domain.OptionID("yes")
	answers := []domain.Answer{{QuestionID: "q1", SelectedOptionID: &optionID}}
	result := make(chan []domain.Answer, 1)
	errc := make(chan error, 1)

	go func() {
		got, err := waiter.Wait(ctx, "batch-1")
		result <- got
		errc <- err
	}()
	if err := waiter.Resume(ctx, "batch-1", answers); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	if err := <-errc; err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if got := <-result; !reflect.DeepEqual(got, answers) {
		t.Fatalf("Wait() = %#v, want %#v", got, answers)
	}
}

func TestMemoryAnswerWaiterWaitCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	waiter := NewMemoryAnswerWaiter()
	errc := make(chan error, 1)

	go func() {
		_, err := waiter.Wait(ctx, "batch-1")
		errc <- err
	}()
	if err := waiter.Cancel(ctx, "batch-1", "session stopped"); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if err := <-errc; !errors.Is(err, ErrWaitCancelled) {
		t.Fatalf("Wait() error = %v, want ErrWaitCancelled", err)
	}
}

func TestWaitReturnsStructuredCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	waiter := NewMemoryAnswerWaiter()
	service := New(nil, waiter)
	errc := make(chan error, 1)

	go func() {
		_, err := service.Wait(ctx, "batch-1")
		errc <- err
	}()
	if err := waiter.Cancel(ctx, "batch-1", "session stopped"); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	err := <-errc
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != apperror.CodeAnswerUserCancelled {
		t.Fatalf("Wait() app error = %#v", err)
	}
}

func batchFixture() domain.Batch {
	return domain.Batch{
		ID:        "batch-1",
		SessionID: "session-1",
		Status:    domain.BatchPending,
		Questions: []domain.Question{
			{
				ID:      "q1",
				BatchID: "batch-1",
				Title:   "Choose",
				Options: []domain.Option{{ID: "yes", Label: "Yes"}, {ID: "no", Label: "No"}},
			},
			{
				ID:          "q2",
				BatchID:     "batch-1",
				Title:       "Why",
				AllowCustom: true,
			},
		},
	}
}

type fakeRepository struct {
	batches     map[domain.BatchID]domain.Batch
	submitCalls int
}

func newFakeRepository(batches ...domain.Batch) *fakeRepository {
	repo := &fakeRepository{batches: map[domain.BatchID]domain.Batch{}}
	for _, batch := range batches {
		repo.batches[batch.ID] = batch
	}
	return repo
}

func (r *fakeRepository) CreateBatch(_ context.Context, batch domain.Batch) error {
	r.batches[batch.ID] = batch
	return nil
}

func (r *fakeRepository) FindBatch(_ context.Context, id domain.BatchID) (domain.Batch, error) {
	batch, ok := r.batches[id]
	if !ok {
		return domain.Batch{}, errors.New("not found")
	}
	return batch, nil
}

func (r *fakeRepository) ListPendingBySession(_ context.Context, sessionID domain.SessionID) ([]domain.Batch, error) {
	batches := make([]domain.Batch, 0)
	for _, batch := range r.batches {
		if batch.SessionID == sessionID && batch.Status == domain.BatchPending {
			batches = append(batches, batch)
		}
	}
	return batches, nil
}

func (r *fakeRepository) SubmitAnswers(_ context.Context, id domain.BatchID, answers []domain.Answer) error {
	r.submitCalls++
	batch, ok := r.batches[id]
	if !ok {
		return errors.New("not found")
	}
	answered, err := (domain.DefaultPolicy{}).ApplyAnswers(batch, answers)
	if err != nil {
		return err
	}
	r.batches[id] = answered
	return nil
}

func (r *fakeRepository) CancelPendingBySession(_ context.Context, sessionID domain.SessionID, reason string) error {
	for id, batch := range r.batches {
		if batch.SessionID != sessionID || batch.Status != domain.BatchPending {
			continue
		}
		cancelled, err := (domain.DefaultPolicy{}).Cancel(batch, reason)
		if err != nil {
			return err
		}
		r.batches[id] = cancelled
	}
	return nil
}

type recordingWaiter struct {
	resumedBatchID   domain.BatchID
	resumedAnswers   []domain.Answer
	resumeCalls      int
	cancelledBatchID domain.BatchID
	cancelReason     string
}

func newRecordingWaiter() *recordingWaiter {
	return &recordingWaiter{}
}

func (w *recordingWaiter) Wait(context.Context, domain.BatchID) ([]domain.Answer, error) {
	return nil, errors.New("unexpected Wait call")
}

func (w *recordingWaiter) Resume(_ context.Context, batchID domain.BatchID, answers []domain.Answer) error {
	w.resumeCalls++
	w.resumedBatchID = batchID
	w.resumedAnswers = append([]domain.Answer(nil), answers...)
	return nil
}

func (w *recordingWaiter) Cancel(_ context.Context, batchID domain.BatchID, reason string) error {
	w.cancelledBatchID = batchID
	w.cancelReason = reason
	return nil
}
