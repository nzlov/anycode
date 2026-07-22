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

func TestSubmitRequestStoresAllAnswers(t *testing.T) {
	repo := newFakeRepository()
	service := New(repo)
	request := pendingRequest()
	repo.requests[request.ID] = request
	optionA := domain.OptionID("a")
	input := SubmitRequestInput{RequestID: request.ID, Answers: []domain.Answer{
		{QuestionID: "q1", SelectedOptionID: &optionA},
		{QuestionID: "q2", CustomAnswer: "custom"},
	}}

	got, err := service.SubmitRequest(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != domain.RequestAnswered || repo.requests[request.ID].Status != domain.RequestAnswered {
		t.Fatalf("request = %#v", got)
	}
	if repo.requests[request.ID].Questions[1].CustomAnswer != "custom" {
		t.Fatalf("answers = %#v", repo.requests[request.ID].Questions)
	}
}

func TestSubmitRequestObservesWaitingDurationWithoutQuestionContent(t *testing.T) {
	repo := newFakeRepository()
	recorder := &questionObservationRecorder{}
	service := New(repo, WithObserver(recorder))
	request := pendingRequest()
	request.CreatedAt = time.Unix(10, 0).UTC()
	repo.requests[request.ID] = request
	service.now = func() time.Time { return time.Unix(13, 0).UTC() }
	optionA := domain.OptionID("a")
	_, err := service.SubmitRequest(context.Background(), SubmitRequestInput{RequestID: request.ID, Answers: []domain.Answer{
		{QuestionID: "q1", SelectedOptionID: &optionA}, {QuestionID: "q2", CustomAnswer: "private answer"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(recorder.items) != 1 || recorder.items[0].Name != "waiting_user" || recorder.items[0].Outcome != "answered" || recorder.items[0].Duration != 3*time.Second {
		t.Fatalf("observations = %#v", recorder.items)
	}
}

type questionObservationRecorder struct {
	items []Observation
}

func (r *questionObservationRecorder) Observe(observation Observation) {
	r.items = append(r.items, observation)
}

func TestSubmitRequestRejectsIncompleteAnswers(t *testing.T) {
	repo := newFakeRepository()
	service := New(repo)
	request := pendingRequest()
	repo.requests[request.ID] = request
	optionA := domain.OptionID("a")

	_, err := service.SubmitRequest(context.Background(), SubmitRequestInput{
		RequestID: request.ID,
		Answers:   []domain.Answer{{QuestionID: "q1", SelectedOptionID: &optionA}},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != apperror.CodeValidationFailed {
		t.Fatalf("error = %#v", err)
	}
}

func TestSubmitRequestIsIdempotentForSameAnswers(t *testing.T) {
	repo := newFakeRepository()
	service := New(repo)
	request := pendingRequest()
	repo.requests[request.ID] = request
	optionA := domain.OptionID("a")
	input := SubmitRequestInput{RequestID: request.ID, Answers: []domain.Answer{
		{QuestionID: "q1", SelectedOptionID: &optionA},
		{QuestionID: "q2", CustomAnswer: "custom"},
	}}
	if _, err := service.SubmitRequest(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SubmitRequest(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if repo.submitCalls != 1 {
		t.Fatalf("submit calls = %d", repo.submitCalls)
	}
}

func TestSubmitRequestRejectsDifferentAnsweredValue(t *testing.T) {
	repo := newFakeRepository()
	service := New(repo)
	request := pendingRequest()
	repo.requests[request.ID] = request
	optionA := domain.OptionID("a")
	if _, err := service.SubmitRequest(context.Background(), SubmitRequestInput{RequestID: request.ID, Answers: []domain.Answer{
		{QuestionID: "q1", SelectedOptionID: &optionA},
		{QuestionID: "q2", CustomAnswer: "first"},
	}}); err != nil {
		t.Fatal(err)
	}
	_, err := service.SubmitRequest(context.Background(), SubmitRequestInput{RequestID: request.ID, Answers: []domain.Answer{
		{QuestionID: "q1", SelectedOptionID: &optionA},
		{QuestionID: "q2", CustomAnswer: "different"},
	}})
	if err == nil {
		t.Fatal("expected mismatched answer error")
	}
}

func TestCreateRequestAndGetRequest(t *testing.T) {
	repo := newFakeRepository()
	service := New(repo)
	service.generateID = sequenceIDs("request-1", "question-1")
	origin := domain.ProcessRunID("process-1")

	created, err := service.CreateRequest(context.Background(), CreateRequestInput{
		SessionID:          "session-1",
		OriginProcessRunID: &origin,
		Questions:          []domain.Question{{Body: "Continue?", Options: []domain.Option{{ID: "yes", Label: "Yes"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID != "request-1" || created.Questions[0].ID != "question-1" || created.OriginProcessRunID == nil {
		t.Fatalf("created = %#v", created)
	}
	got, err := service.GetRequest(context.Background(), created.ID)
	if err != nil || got.ID != created.ID {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestCreateRequestRejectsBlankQuestionBody(t *testing.T) {
	service := New(newFakeRepository())
	_, err := service.CreateRequest(context.Background(), CreateRequestInput{
		SessionID: "session-1",
		Questions: []domain.Question{{Body: "  "}},
	})
	if err == nil || err.Error() != "question body is required" {
		t.Fatalf("error = %v", err)
	}
}

func TestCreateRequestWithStableIDReturnsExistingRequest(t *testing.T) {
	repo := newFakeRepository()
	service := New(repo)
	service.generateID = sequenceIDs("question-1", "question-2")
	input := CreateRequestInput{
		RequestID: "merge-failure-command-1",
		SessionID: "session-1",
		Questions: []domain.Question{{Body: "Resolve merge?"}},
	}
	first, err := service.CreateRequest(context.Background(), input)
	if err != nil {
		t.Fatalf("first CreateRequest() error = %v", err)
	}
	second, err := service.CreateRequest(context.Background(), input)
	if err != nil {
		t.Fatalf("second CreateRequest() error = %v", err)
	}
	if first.ID != input.RequestID || second.ID != first.ID || second.Questions[0].ID != first.Questions[0].ID {
		t.Fatalf("first=%#v second=%#v", first, second)
	}
}

func TestCreateRequestWithStableIDRejectsNonPendingExistingRequest(t *testing.T) {
	repo := newFakeRepository()
	service := New(repo)
	service.generateID = sequenceIDs("question-1", "question-2")
	input := CreateRequestInput{
		RequestID: "merge-failure-command-1",
		SessionID: "session-1",
		Questions: []domain.Question{{Body: "Resolve merge?"}},
	}
	if _, err := service.CreateRequest(context.Background(), input); err != nil {
		t.Fatalf("first CreateRequest() error = %v", err)
	}
	existing := repo.requests[input.RequestID]
	existing.Status = domain.RequestCancelled
	repo.requests[input.RequestID] = existing
	if _, err := service.CreateRequest(context.Background(), input); err == nil {
		t.Fatal("CreateRequest() should reject a cancelled stable request")
	}
}

func TestGetRequestReturnsStructuredNotFound(t *testing.T) {
	service := New(newFakeRepository())
	_, err := service.GetRequest(context.Background(), "missing")
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != apperror.CodeNotFound {
		t.Fatalf("error = %#v", err)
	}
}

func TestListPendingRequestsBySessionFiltersAnsweredRequests(t *testing.T) {
	repo := newFakeRepository()
	pending := pendingRequest()
	answered := pendingRequest()
	answered.ID = "request-answered"
	answered.Status = domain.RequestAnswered
	repo.requests[pending.ID] = pending
	repo.requests[answered.ID] = answered
	service := New(repo)
	got, err := service.ListPendingRequestsBySession(context.Background(), pending.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != pending.ID {
		t.Fatalf("pending = %#v", got)
	}
}

func TestQuestionRequestUpdatesPublishesLiveChanges(t *testing.T) {
	repo := newFakeRepository()
	service := New(repo)
	service.generateID = sequenceIDs("request-1", "question-1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates, err := service.QuestionRequestUpdates(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateRequest(context.Background(), CreateRequestInput{
		SessionID: "session-1",
		Questions: []domain.Question{{Body: "Continue?", Options: []domain.Option{{ID: "yes", Label: "Yes"}}}},
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

func TestQuestionRequestUpdatesDoesNotDropBurst(t *testing.T) {
	service := New(newFakeRepository())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates, err := service.QuestionRequestUpdates(ctx, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	const count = 32
	for i := 0; i < count; i++ {
		service.PublishRequest(RequestDTO{ID: domain.RequestID(string(rune('a' + i))), SessionID: "session-1", Status: domain.RequestPending})
	}
	for i := 0; i < count; i++ {
		select {
		case <-updates:
		case <-time.After(time.Second):
			t.Fatalf("received %d of %d updates", i, count)
		}
	}
}

func TestQuestionRequestUpdatesCloseWithContext(t *testing.T) {
	service := New(newFakeRepository())
	ctx, cancel := context.WithCancel(context.Background())
	updates, err := service.QuestionRequestUpdates(ctx, "session-1")
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
	request := pendingRequest()
	repo.requests[request.ID] = request
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates, err := service.QuestionRequestUpdates(ctx, request.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.CancelPendingRequestsBySession(context.Background(), request.SessionID, "session stopped"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-updates:
		if got.Status != domain.RequestCancelled {
			t.Fatalf("update = %#v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation update not published")
	}
}

func pendingRequest() domain.Request {
	return domain.Request{
		ID:        "request-1",
		SessionID: "session-1",
		Status:    domain.RequestPending,
		Questions: []domain.Question{
			{ID: "q1", Options: []domain.Option{{ID: "a", Label: "A"}}},
			{ID: "q2"},
		},
	}
}

type fakeRepository struct {
	mu          sync.Mutex
	requests    map[domain.RequestID]domain.Request
	submitCalls int
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{requests: map[domain.RequestID]domain.Request{}}
}

func (r *fakeRepository) CreateRequest(_ context.Context, request domain.Request) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.requests[request.ID]; exists {
		return errors.New("duplicate request")
	}
	r.requests[request.ID] = request
	return nil
}

func (r *fakeRepository) FindRequest(_ context.Context, id domain.RequestID) (domain.Request, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	request, ok := r.requests[id]
	if !ok {
		return domain.Request{}, errors.New("not found")
	}
	return request, nil
}

func (r *fakeRepository) ListPendingRequestsBySession(_ context.Context, sessionID domain.SessionID) ([]domain.Request, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []domain.Request
	for _, request := range r.requests {
		if request.SessionID == sessionID && request.Status == domain.RequestPending {
			result = append(result, request)
		}
	}
	return result, nil
}

func (r *fakeRepository) SubmitAnswers(_ context.Context, id domain.RequestID, answers []domain.Answer) (domain.Request, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	request, ok := r.requests[id]
	if !ok {
		return domain.Request{}, false, errors.New("not found")
	}
	if request.Status != domain.RequestPending {
		return request, false, nil
	}
	r.submitCalls++
	byQuestion := map[domain.QuestionID]domain.Answer{}
	for _, answer := range answers {
		byQuestion[answer.QuestionID] = answer
	}
	for i := range request.Questions {
		answer := byQuestion[request.Questions[i].ID]
		request.Questions[i].SelectedOptionID = answer.SelectedOptionID
		request.Questions[i].CustomAnswer = answer.CustomAnswer
		request.Questions[i].Answer = answer.Payload
		request.Questions[i].Status = "answered"
	}
	request.Status = domain.RequestAnswered
	r.requests[id] = request
	return request, true, nil
}

func (r *fakeRepository) CancelPendingRequestsBySession(_ context.Context, sessionID domain.SessionID, _ string) ([]domain.Request, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var result []domain.Request
	for id, request := range r.requests {
		if request.SessionID != sessionID || request.Status != domain.RequestPending {
			continue
		}
		request.Status = domain.RequestCancelled
		r.requests[id] = request
		result = append(result, request)
	}
	return result, nil
}

func (r *fakeRepository) CancelPendingRequest(_ context.Context, id domain.RequestID, _ string) (domain.Request, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	request, ok := r.requests[id]
	if !ok {
		return domain.Request{}, false, errors.New("not found")
	}
	if request.Status != domain.RequestPending {
		return request, false, nil
	}
	request.Status = domain.RequestCancelled
	r.requests[id] = request
	return request, true, nil
}

func (r *fakeRepository) FindLatestRequestBySession(_ context.Context, sessionID domain.SessionID) (domain.Request, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var latest domain.Request
	var found bool
	for _, request := range r.requests {
		if request.SessionID == sessionID && (!found || request.CreatedAt.After(latest.CreatedAt)) {
			latest, found = request, true
		}
	}
	return latest, found, nil
}

func (r *fakeRepository) FindPendingRequestByOriginProcessRun(_ context.Context, processRunID domain.ProcessRunID) (domain.Request, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, request := range r.requests {
		if request.OriginProcessRunID != nil && *request.OriginProcessRunID == processRunID && request.Status == domain.RequestPending {
			return request, true, nil
		}
	}
	return domain.Request{}, false, nil
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
