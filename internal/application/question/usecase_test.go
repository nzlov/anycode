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
	waiter := newRecordingWaiter()
	service, repo := newWaitableQuestionService(t, waiter)

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
	waiter := newRecordingWaiter()
	service, repo := newWaitableQuestionService(t, waiter)
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

func TestSubmitBatchIdempotentRetryDoesNotRequeueConsumedAnswers(t *testing.T) {
	ctx := context.Background()
	waiter := NewMemoryAnswerWaiter()
	service, _ := newWaitableQuestionService(t, waiter)
	optionID := domain.OptionID("yes")
	input := SubmitBatchInput{
		BatchID: "batch-1",
		Answers: []domain.Answer{
			{QuestionID: "q1", SelectedOptionID: &optionID},
			{QuestionID: "q2", CustomAnswer: "ship it"},
		},
	}

	if _, err := service.SubmitBatch(ctx, input); err != nil {
		t.Fatalf("first SubmitBatch() error = %v", err)
	}
	got, err := service.Wait(ctx, input.BatchID)
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if !reflect.DeepEqual(got, input.Answers) {
		t.Fatalf("Wait() = %#v, want %#v", got, input.Answers)
	}
	if _, err := service.SubmitBatch(ctx, input); err != nil {
		t.Fatalf("second SubmitBatch() error = %v", err)
	}
	waiter.mu.Lock()
	entries := len(waiter.entries)
	waiter.mu.Unlock()
	if entries != 0 {
		t.Fatalf("waiter entries = %d, want 0", entries)
	}
}

func TestSubmitBatchRetriesWaiterResumeForAnsweredBatch(t *testing.T) {
	ctx := context.Background()
	waiter := newRecordingWaiter()
	waiter.resumeErrors = []error{errors.New("temporary resume failure"), nil}
	service, repo := newWaitableQuestionService(t, waiter)
	optionID := domain.OptionID("yes")
	input := SubmitBatchInput{
		BatchID: "batch-1",
		Answers: []domain.Answer{
			{QuestionID: "q1", SelectedOptionID: &optionID},
			{QuestionID: "q2", CustomAnswer: "ship it"},
		},
	}

	if _, err := service.SubmitBatch(ctx, input); err == nil || !strings.Contains(err.Error(), "temporary resume failure") {
		t.Fatalf("first SubmitBatch() error = %v", err)
	}
	if repo.batches["batch-1"].Status != domain.BatchAnswered {
		t.Fatalf("stored batch status = %q", repo.batches["batch-1"].Status)
	}
	got, err := service.SubmitBatch(ctx, input)
	if err != nil {
		t.Fatalf("second SubmitBatch() error = %v", err)
	}
	if got.Status != domain.BatchAnswered {
		t.Fatalf("Status = %q", got.Status)
	}
	if waiter.resumeCalls != 2 {
		t.Fatalf("Resume calls = %d, want 2", waiter.resumeCalls)
	}
}

func TestSubmitBatchSerializesWaiterRecoveryForConcurrentRetries(t *testing.T) {
	waiter := newRecordingWaiter()
	resumeStarted := make(chan struct{})
	releaseResume := make(chan struct{})
	waiter.beforeResume = func(call int) {
		if call != 1 {
			return
		}
		close(resumeStarted)
		<-releaseResume
	}
	waiter.resumeErrors = []error{errors.New("temporary resume failure"), nil}
	service, _ := newWaitableQuestionService(t, waiter)
	optionID := domain.OptionID("yes")
	input := SubmitBatchInput{
		BatchID: "batch-1",
		Answers: []domain.Answer{
			{QuestionID: "q1", SelectedOptionID: &optionID},
			{QuestionID: "q2", CustomAnswer: "ship it"},
		},
	}

	firstDone := make(chan error, 1)
	go func() {
		_, err := service.SubmitBatch(context.Background(), input)
		firstDone <- err
	}()
	<-resumeStarted
	secondDone := make(chan error, 1)
	go func() {
		_, err := service.SubmitBatch(context.Background(), input)
		secondDone <- err
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("concurrent SubmitBatch() returned before first delivery completed: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(releaseResume)
	if err := <-firstDone; err == nil || !strings.Contains(err.Error(), "temporary resume failure") {
		t.Fatalf("first SubmitBatch() error = %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second SubmitBatch() error = %v", err)
	}
	if waiter.resumeCalls != 2 {
		t.Fatalf("Resume calls = %d, want 2", waiter.resumeCalls)
	}
}

func TestSubmitBatchResumesWaiterRegisteredDuringPersistence(t *testing.T) {
	repo := newFakeRepository(batchFixture())
	submitReady := make(chan struct{})
	releaseSubmit := make(chan struct{})
	repo.beforeSubmit = func() {
		close(submitReady)
		<-releaseSubmit
	}
	waitReadPending := make(chan struct{})
	findCalls := 0
	repo.afterFind = func(batch domain.Batch) {
		findCalls++
		if findCalls == 2 && batch.Status == domain.BatchPending {
			close(waitReadPending)
		}
	}
	waiter := NewMemoryAnswerWaiter()
	service := New(repo, waiter)
	optionID := domain.OptionID("yes")
	input := SubmitBatchInput{
		BatchID: "batch-1",
		Answers: []domain.Answer{
			{QuestionID: "q1", SelectedOptionID: &optionID},
			{QuestionID: "q2", CustomAnswer: "ship it"},
		},
	}

	submitDone := make(chan error, 1)
	go func() {
		_, err := service.SubmitBatch(context.Background(), input)
		submitDone <- err
	}()
	<-submitReady
	waitCtx, cancelWait := context.WithTimeout(context.Background(), time.Second)
	defer cancelWait()
	waitDone := make(chan struct {
		answers []domain.Answer
		err     error
	}, 1)
	go func() {
		answers, err := service.Wait(waitCtx, "batch-1")
		waitDone <- struct {
			answers []domain.Answer
			err     error
		}{answers: answers, err: err}
	}()
	<-waitReadPending
	close(releaseSubmit)
	if err := <-submitDone; err != nil {
		t.Fatalf("SubmitBatch() error = %v", err)
	}
	waitResult := <-waitDone
	if waitResult.err != nil {
		t.Fatalf("Wait() error = %v", waitResult.err)
	}
	if !reflect.DeepEqual(waitResult.answers, input.Answers) {
		t.Fatalf("Wait() = %#v, want %#v", waitResult.answers, input.Answers)
	}
}

func TestSubmitBatchWithoutWaitingConsumerDoesNotQueueAnswers(t *testing.T) {
	repo := newFakeRepository(batchFixture())
	waiter := NewMemoryAnswerWaiter()
	service := New(repo, waiter)
	optionID := domain.OptionID("yes")

	if _, err := service.SubmitBatch(context.Background(), SubmitBatchInput{
		BatchID: "batch-1",
		Answers: []domain.Answer{
			{QuestionID: "q1", SelectedOptionID: &optionID},
			{QuestionID: "q2", CustomAnswer: "ship it"},
		},
	}); err != nil {
		t.Fatalf("SubmitBatch() error = %v", err)
	}
	waiter.mu.Lock()
	entries := len(waiter.entries)
	waiter.mu.Unlock()
	if entries != 0 {
		t.Fatalf("waiter entries = %d, want 0", entries)
	}
}

func TestSubmitBatchAfterWaitCancellationDoesNotQueueAnswers(t *testing.T) {
	repo := newFakeRepository(batchFixture())
	waiter := NewMemoryAnswerWaiter()
	service := New(repo, waiter)
	waitCtx, cancelWait := context.WithCancel(context.Background())
	cancelWait()
	if _, err := service.Wait(waitCtx, "batch-1"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Wait() error = %v, want context.Canceled", err)
	}
	optionID := domain.OptionID("yes")
	if _, err := service.SubmitBatch(context.Background(), SubmitBatchInput{
		BatchID: "batch-1",
		Answers: []domain.Answer{
			{QuestionID: "q1", SelectedOptionID: &optionID},
			{QuestionID: "q2", CustomAnswer: "ship it"},
		},
	}); err != nil {
		t.Fatalf("SubmitBatch() error = %v", err)
	}
	waiter.mu.Lock()
	entries := len(waiter.entries)
	waiter.mu.Unlock()
	if entries != 0 {
		t.Fatalf("waiter entries = %d, want 0", entries)
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

func TestSubmitBatchRejectsDifferentPayloadWhenAlreadyAnswered(t *testing.T) {
	tests := []struct {
		name         string
		firstPayload map[string]any
		retryPayload map[string]any
	}{
		{
			name:         "persisted payload is empty",
			retryPayload: map[string]any{"choice": "yes"},
		},
		{
			name:         "retry payload is empty",
			firstPayload: map[string]any{"choice": "yes"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := newFakeRepository(batchFixture())
			service := New(repo, nil)
			optionID := domain.OptionID("yes")
			answers := func(payload map[string]any) []domain.Answer {
				return []domain.Answer{
					{QuestionID: "q1", SelectedOptionID: &optionID, Payload: payload},
					{QuestionID: "q2", CustomAnswer: "ship it"},
				}
			}

			if _, err := service.SubmitBatch(context.Background(), SubmitBatchInput{
				BatchID: "batch-1",
				Answers: answers(test.firstPayload),
			}); err != nil {
				t.Fatalf("first SubmitBatch() error = %v", err)
			}

			_, err := service.SubmitBatch(context.Background(), SubmitBatchInput{
				BatchID: "batch-1",
				Answers: answers(test.retryPayload),
			})
			if err == nil || !strings.Contains(err.Error(), "already answered with different answers") {
				t.Fatalf("second SubmitBatch() error = %v", err)
			}
		})
	}
}

func TestSubmitBatchDoesNotReviveBatchCancelledDuringPersist(t *testing.T) {
	ctx := context.Background()
	repo := newFakeRepository(batchFixture())
	submitLoaded := make(chan struct{})
	continueSubmit := make(chan struct{})
	repo.beforeSubmit = func() {
		close(submitLoaded)
		<-continueSubmit
	}
	service := New(repo, nil)
	source, err := service.QuestionBatchUpdates(ctx, "session-1")
	if err != nil {
		t.Fatalf("QuestionBatchUpdates() error = %v", err)
	}
	optionID := domain.OptionID("yes")
	submitErr := make(chan error, 1)
	go func() {
		_, err := service.SubmitBatch(ctx, SubmitBatchInput{
			BatchID: "batch-1",
			Answers: []domain.Answer{
				{QuestionID: "q1", SelectedOptionID: &optionID},
				{QuestionID: "q2", CustomAnswer: "ship it"},
			},
		})
		submitErr <- err
	}()
	<-submitLoaded
	if err := service.CancelPendingBySession(ctx, "session-1", "session stopped"); err != nil {
		t.Fatalf("CancelPendingBySession() error = %v", err)
	}
	if got := <-source; got.Status != domain.BatchCancelled {
		t.Fatalf("cancelled update = %#v", got)
	}
	close(continueSubmit)
	if err := <-submitErr; err == nil {
		t.Fatal("SubmitBatch() succeeded after cancellation won the transition")
	}
	select {
	case got := <-source:
		t.Fatalf("unexpected update after cancellation = %#v", got)
	case <-time.After(20 * time.Millisecond):
	}
	if got := repo.batches["batch-1"].Status; got != domain.BatchCancelled {
		t.Fatalf("stored batch status = %q, want cancelled", got)
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

func TestQuestionBatchUpdatesOnlySendsLiveChanges(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	existing := batchFixture()
	repo := newFakeRepository(existing)
	service := New(repo, nil)
	service.generateID = func() (string, error) { return "batch-created", nil }

	source, err := service.QuestionBatchUpdates(ctx, "session-1")
	if err != nil {
		t.Fatalf("QuestionBatchUpdates() error = %v", err)
	}
	select {
	case got := <-source:
		t.Fatalf("unexpected snapshot batch = %#v", got)
	case <-time.After(20 * time.Millisecond):
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

func TestQuestionBatchUpdatesDoesNotDropBurstBeforeConsumerReads(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := New(newFakeRepository(), nil)
	source, err := service.QuestionBatchUpdates(ctx, "session-1")
	if err != nil {
		t.Fatalf("QuestionBatchUpdates() error = %v", err)
	}

	pending := toDTO(batchFixture())
	answered := pending
	answered.Status = domain.BatchAnswered
	service.publish(pending)
	service.publish(answered)

	for index, want := range []domain.BatchStatus{domain.BatchPending, domain.BatchAnswered} {
		select {
		case got := <-source:
			if got.ID != pending.ID || got.Status != want {
				t.Fatalf("update %d = %#v, want status %q", index, got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for update %d", index)
		}
	}
}

func TestPendingSubscriberIgnoresUpdatesAfterClose(t *testing.T) {
	subscriber := newPendingSubscriber()
	subscriber.close()

	if subscriber.send(BatchDTO{ID: "batch-late"}) {
		t.Fatal("send after close returned true")
	}
	if _, ok := <-subscriber.updates; ok {
		t.Fatal("subscriber channel remained open after close")
	}
}

func TestCancelPendingBySessionCancelsRepositoryAndWaiters(t *testing.T) {
	other := batchFixture()
	other.ID = "batch-other"
	other.SessionID = "session-2"
	waiter := newRecordingWaiter()
	service, repo := newWaitableQuestionService(t, waiter)
	repo.batches[other.ID] = other
	pending := repo.batches["batch-1"]

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

func TestCancelPendingBySessionWithoutWaitingConsumerDoesNotCreateWaiterEntry(t *testing.T) {
	repo := newFakeRepository(batchFixture())
	waiter := NewMemoryAnswerWaiter()
	service := New(repo, waiter)

	if err := service.CancelPendingBySession(context.Background(), "session-1", "session stopped"); err != nil {
		t.Fatalf("CancelPendingBySession() error = %v", err)
	}
	waiter.mu.Lock()
	entries := len(waiter.entries)
	waiter.mu.Unlock()
	if entries != 0 {
		t.Fatalf("waiter entries = %d, want 0", entries)
	}
}

func TestMemoryAnswerWaiterWaitResume(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	waiter := NewMemoryAnswerWaiter()
	if err := waiter.Prepare(ctx, "batch-1"); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
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
	if err := waiter.Prepare(ctx, "batch-1"); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
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

func TestMemoryAnswerWaiterWaitDoesNotRecreateForgottenEntry(t *testing.T) {
	waiter := NewMemoryAnswerWaiter()
	if err := waiter.Prepare(context.Background(), "batch-1"); err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	waiter.Forget("batch-1")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := waiter.Wait(ctx, "batch-1")
	if !errors.Is(err, ErrWaitCancelled) {
		t.Fatalf("Wait() error = %v, want ErrWaitCancelled", err)
	}
	waiter.mu.Lock()
	entries := len(waiter.entries)
	waiter.mu.Unlock()
	if entries != 0 {
		t.Fatalf("waiter entries = %d, want 0", entries)
	}
}

func TestWaitReturnsStructuredCancellation(t *testing.T) {
	cancelled := batchFixture()
	cancelled.Status = domain.BatchCancelled
	waiter := NewMemoryAnswerWaiter()
	service := New(newFakeRepository(cancelled), waiter)
	_, err := service.Wait(context.Background(), "batch-1")
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
	batches           map[domain.BatchID]domain.Batch
	submitCalls       int
	beforeSubmit      func()
	beforeListPending func()
	afterFind         func(batch domain.Batch)
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
	if r.afterFind != nil {
		r.afterFind(batch)
	}
	return batch, nil
}

func (r *fakeRepository) ListPendingBySession(_ context.Context, sessionID domain.SessionID) ([]domain.Batch, error) {
	if r.beforeListPending != nil {
		r.beforeListPending()
	}
	batches := make([]domain.Batch, 0)
	for _, batch := range r.batches {
		if batch.SessionID == sessionID && batch.Status == domain.BatchPending {
			batches = append(batches, batch)
		}
	}
	return batches, nil
}

func (r *fakeRepository) SubmitAnswers(_ context.Context, id domain.BatchID, answers []domain.Answer) (domain.Batch, bool, error) {
	r.submitCalls++
	_, ok := r.batches[id]
	if !ok {
		return domain.Batch{}, false, errors.New("not found")
	}
	if r.beforeSubmit != nil {
		r.beforeSubmit()
	}
	batch := r.batches[id]
	if batch.Status != domain.BatchPending {
		return batch, false, nil
	}
	answered, err := (domain.DefaultPolicy{}).ApplyAnswers(batch, answers)
	if err != nil {
		return domain.Batch{}, false, err
	}
	r.batches[id] = answered
	return answered, true, nil
}

func (r *fakeRepository) CancelPendingBySession(_ context.Context, sessionID domain.SessionID, reason string) ([]domain.Batch, error) {
	cancelledBatches := []domain.Batch{}
	for id, batch := range r.batches {
		if batch.SessionID != sessionID || batch.Status != domain.BatchPending {
			continue
		}
		cancelled, err := (domain.DefaultPolicy{}).Cancel(batch, reason)
		if err != nil {
			return nil, err
		}
		r.batches[id] = cancelled
		cancelledBatches = append(cancelledBatches, cancelled)
	}
	return cancelledBatches, nil
}

type recordingWaiter struct {
	resumedBatchID   domain.BatchID
	resumedAnswers   []domain.Answer
	resumeCalls      int
	resumeErrors     []error
	beforeResume     func(call int)
	cancelledBatchID domain.BatchID
	cancelReason     string
}

func newRecordingWaiter() *recordingWaiter {
	return &recordingWaiter{}
}

func (w *recordingWaiter) Prepare(context.Context, domain.BatchID) error {
	return nil
}

func (w *recordingWaiter) Wait(context.Context, domain.BatchID) ([]domain.Answer, error) {
	return nil, errors.New("unexpected Wait call")
}

func (w *recordingWaiter) Resume(_ context.Context, batchID domain.BatchID, answers []domain.Answer) error {
	w.resumeCalls++
	if w.beforeResume != nil {
		w.beforeResume(w.resumeCalls)
	}
	w.resumedBatchID = batchID
	w.resumedAnswers = append([]domain.Answer(nil), answers...)
	if len(w.resumeErrors) > 0 {
		err := w.resumeErrors[0]
		w.resumeErrors = w.resumeErrors[1:]
		return err
	}
	return nil
}

func newWaitableQuestionService(t *testing.T, waiter domain.AnswerWaiter) (*Service, *fakeRepository) {
	t.Helper()
	repo := newFakeRepository()
	service := New(repo, waiter)
	service.generateID = func() (string, error) { return "batch-1", nil }
	fixture := batchFixture()
	if _, err := service.CreateBatch(context.Background(), CreateBatchInput{
		SessionID: fixture.SessionID,
		Questions: fixture.Questions,
	}); err != nil {
		t.Fatalf("CreateBatch() error = %v", err)
	}
	service.ensureWaitDelivery("batch-1")
	return service, repo
}

func (w *recordingWaiter) Cancel(_ context.Context, batchID domain.BatchID, reason string) error {
	w.cancelledBatchID = batchID
	w.cancelReason = reason
	return nil
}

func (w *recordingWaiter) Forget(domain.BatchID) {}
