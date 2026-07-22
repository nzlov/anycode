package question

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/nzlov/anycode/internal/application/apperror"
	domain "github.com/nzlov/anycode/internal/domain/question"
)

type UseCase interface {
	CreateRequest(ctx context.Context, input CreateRequestInput) (RequestDTO, error)
	SubmitRequest(ctx context.Context, input SubmitRequestInput) (RequestDTO, error)
	GetRequest(ctx context.Context, id domain.RequestID) (RequestDTO, error)
	ListPendingRequestsBySession(ctx context.Context, sessionID domain.SessionID) ([]RequestDTO, error)
	QuestionRequestUpdates(ctx context.Context, sessionID domain.SessionID) (<-chan RequestDTO, error)
	CancelPendingRequestsBySession(ctx context.Context, sessionID domain.SessionID, reason string) error
}

type CreateRequestInput struct {
	RequestID          domain.RequestID
	SessionID          domain.SessionID
	OriginProcessRunID *domain.ProcessRunID
	Questions          []domain.Question
}

type SubmitRequestInput struct {
	RequestID domain.RequestID
	Answers   []domain.Answer
}

type RequestDTO struct {
	ID                 domain.RequestID
	SessionID          domain.SessionID
	OriginProcessRunID *domain.ProcessRunID
	Status             domain.RequestStatus
	Questions          []domain.Question
	Created            bool
}

type Service struct {
	repo       domain.Repository
	policy     domain.Policy
	now        func() time.Time
	generateID func() (string, error)
	broker     *pendingBroker
	observer   Observer
}

type Observation struct {
	Name     string
	Outcome  string
	Duration time.Duration
}

type Observer interface {
	Observe(Observation)
}

type Option func(*Service)

func WithObserver(observer Observer) Option {
	return func(service *Service) { service.observer = observer }
}

func New(repo domain.Repository, options ...Option) *Service {
	service := &Service{
		repo:       repo,
		policy:     domain.DefaultPolicy{},
		now:        time.Now,
		generateID: generateID,
		broker:     newPendingBroker(),
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) CreateRequest(ctx context.Context, input CreateRequestInput) (RequestDTO, error) {
	if s == nil {
		return RequestDTO{}, errors.New("question usecase: nil service")
	}
	if s.repo == nil {
		return RequestDTO{}, errors.New("question repository is required")
	}
	if input.SessionID == "" {
		return RequestDTO{}, errors.New("session id is required")
	}
	if len(input.Questions) == 0 {
		return RequestDTO{}, errors.New("questions are required")
	}
	requestID := string(input.RequestID)
	if requestID == "" {
		var err error
		requestID, err = s.generateID()
		if err != nil {
			return RequestDTO{}, fmt.Errorf("generate question request id: %w", err)
		}
	}
	questions := make([]domain.Question, len(input.Questions))
	for i, item := range input.Questions {
		questions[i] = item
		if questions[i].ID == "" {
			id, err := s.generateID()
			if err != nil {
				return RequestDTO{}, fmt.Errorf("generate question id: %w", err)
			}
			questions[i].ID = domain.QuestionID(id)
		}
		questions[i].RequestID = domain.RequestID(requestID)
	}
	request := domain.Request{
		ID:                 domain.RequestID(requestID),
		SessionID:          input.SessionID,
		OriginProcessRunID: input.OriginProcessRunID,
		Status:             domain.RequestPending,
		Questions:          questions,
		CreatedAt:          s.now(),
	}
	if err := s.repo.CreateRequest(ctx, request); err != nil {
		if input.RequestID != "" {
			existing, findErr := s.repo.FindRequest(ctx, input.RequestID)
			if findErr == nil && existing.SessionID == input.SessionID && existing.Status == domain.RequestPending {
				return toDTO(existing), nil
			}
		}
		return RequestDTO{}, fmt.Errorf("create question request: %w", err)
	}
	dto := toDTO(request)
	dto.Created = true
	s.publish(dto)
	return dto, nil
}

func (s *Service) SubmitRequest(ctx context.Context, input SubmitRequestInput) (RequestDTO, error) {
	if s == nil {
		return RequestDTO{}, errors.New("question usecase: nil service")
	}
	if s.repo == nil {
		return RequestDTO{}, errors.New("question repository is required")
	}
	if s.policy == nil {
		return RequestDTO{}, errors.New("question policy is required")
	}
	request, err := s.repo.FindRequest(ctx, input.RequestID)
	if err != nil {
		return RequestDTO{}, apperror.Wrap(err, apperror.CodeNotFound, apperror.CategoryValidationError, "question request not found").WithDetails(map[string]any{"requestId": string(input.RequestID)})
	}
	if request.Status == domain.RequestAnswered {
		if err := ensureAnswersMatchAnsweredRequest(request, input.Answers); err != nil {
			return RequestDTO{}, apperror.Wrap(err, apperror.CodeValidationFailed, apperror.CategoryValidationError, "question answers do not match existing answers")
		}
		return toDTO(request), nil
	}
	if err := s.policy.CanSubmit(request, input.Answers); err != nil {
		return RequestDTO{}, apperror.Wrap(err, apperror.CodeValidationFailed, apperror.CategoryValidationError, "question answers are invalid").WithDetails(map[string]any{"requestId": string(input.RequestID)})
	}
	persisted, transitioned, err := s.repo.SubmitAnswers(ctx, input.RequestID, input.Answers)
	if err != nil {
		return RequestDTO{}, fmt.Errorf("submit question answers: %w", err)
	}
	if !transitioned {
		if persisted.Status == domain.RequestAnswered {
			if err := ensureAnswersMatchAnsweredRequest(persisted, input.Answers); err != nil {
				return RequestDTO{}, apperror.Wrap(err, apperror.CodeValidationFailed, apperror.CategoryValidationError, "question answers do not match existing answers")
			}
			return toDTO(persisted), nil
		}
		return RequestDTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "question request is no longer pending").WithDetails(map[string]any{"requestId": string(input.RequestID), "status": string(persisted.Status)})
	}
	dto := toDTO(persisted)
	if s.observer != nil {
		duration := s.now().Sub(request.CreatedAt)
		if duration < 0 {
			duration = 0
		}
		s.observer.Observe(Observation{Name: "waiting_user", Outcome: "answered", Duration: duration})
	}
	s.publish(dto)
	return dto, nil
}

func ensureAnswersMatchAnsweredRequest(request domain.Request, answers []domain.Answer) error {
	if len(answers) != len(request.Questions) {
		return fmt.Errorf("question request %s is already answered with different answers", request.ID)
	}
	byQuestion := make(map[domain.QuestionID]domain.Answer, len(answers))
	for _, answer := range answers {
		if _, exists := byQuestion[answer.QuestionID]; exists {
			return fmt.Errorf("question %s has duplicate answers", answer.QuestionID)
		}
		byQuestion[answer.QuestionID] = answer
	}
	for _, question := range request.Questions {
		answer, ok := byQuestion[question.ID]
		if !ok {
			return fmt.Errorf("question request %s is already answered with different answers", request.ID)
		}
		if !sameOptionID(question.SelectedOptionID, answer.SelectedOptionID) {
			return fmt.Errorf("question request %s is already answered with different answers", request.ID)
		}
		if strings.TrimSpace(question.CustomAnswer) != strings.TrimSpace(answer.CustomAnswer) {
			return fmt.Errorf("question request %s is already answered with different answers", request.ID)
		}
		if (len(answer.Payload) > 0 || len(question.Answer) > 0) && !reflect.DeepEqual(question.Answer, answer.Payload) {
			return fmt.Errorf("question request %s is already answered with different answers", request.ID)
		}
	}
	return nil
}

func sameOptionID(left, right *domain.OptionID) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return *left == *right
	}
}

func (s *Service) GetRequest(ctx context.Context, id domain.RequestID) (RequestDTO, error) {
	if s == nil {
		return RequestDTO{}, errors.New("question usecase: nil service")
	}
	if s.repo == nil {
		return RequestDTO{}, errors.New("question repository is required")
	}
	request, err := s.repo.FindRequest(ctx, id)
	if err != nil {
		return RequestDTO{}, apperror.Wrap(err, apperror.CodeNotFound, apperror.CategoryValidationError, "question request not found").WithDetails(map[string]any{"requestId": string(id)})
	}
	return toDTO(request), nil
}

func (s *Service) ListPendingRequestsBySession(ctx context.Context, sessionID domain.SessionID) ([]RequestDTO, error) {
	if s == nil {
		return nil, errors.New("question usecase: nil service")
	}
	if s.repo == nil {
		return nil, errors.New("question repository is required")
	}
	requests, err := s.repo.ListPendingRequestsBySession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list pending question requests: %w", err)
	}
	dtos := make([]RequestDTO, 0, len(requests))
	for _, request := range requests {
		dtos = append(dtos, toDTO(request))
	}
	return dtos, nil
}

func (s *Service) QuestionRequestUpdates(ctx context.Context, sessionID domain.SessionID) (<-chan RequestDTO, error) {
	if s == nil || s.broker == nil {
		return nil, errors.New("question update broker is required")
	}
	return s.broker.subscribe(ctx, sessionID)
}

func (s *Service) CancelPendingRequestsBySession(ctx context.Context, sessionID domain.SessionID, reason string) error {
	if s == nil {
		return errors.New("question usecase: nil service")
	}
	if s.repo == nil {
		return errors.New("question repository is required")
	}
	cancelled, err := s.repo.CancelPendingRequestsBySession(ctx, sessionID, reason)
	if err != nil {
		return fmt.Errorf("cancel pending question requests: %w", err)
	}
	for _, request := range cancelled {
		s.publish(toDTO(request))
	}
	return nil
}

func toDTO(request domain.Request) RequestDTO {
	return RequestDTO{
		ID:                 request.ID,
		SessionID:          request.SessionID,
		OriginProcessRunID: request.OriginProcessRunID,
		Status:             request.Status,
		Questions:          append([]domain.Question(nil), request.Questions...),
	}
}

func (s *Service) PublishRequest(request RequestDTO) {
	s.publish(request)
}

func generateID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (s *Service) publish(request RequestDTO) {
	if s != nil && s.broker != nil {
		s.broker.publish(request)
	}
}

type pendingBroker struct {
	mu          sync.Mutex
	subscribers map[domain.SessionID]map[*pendingSubscriber]struct{}
}

func newPendingBroker() *pendingBroker {
	return &pendingBroker{subscribers: map[domain.SessionID]map[*pendingSubscriber]struct{}{}}
}

func (b *pendingBroker) subscribe(ctx context.Context, sessionID domain.SessionID) (<-chan RequestDTO, error) {
	subscriber := newPendingSubscriber()
	b.mu.Lock()
	if b.subscribers == nil {
		b.subscribers = map[domain.SessionID]map[*pendingSubscriber]struct{}{}
	}
	if b.subscribers[sessionID] == nil {
		b.subscribers[sessionID] = map[*pendingSubscriber]struct{}{}
	}
	b.subscribers[sessionID][subscriber] = struct{}{}
	b.mu.Unlock()

	if err := ctx.Err(); err != nil {
		b.unsubscribe(sessionID, subscriber)
		return nil, err
	}
	if !subscriber.start() {
		b.unsubscribe(sessionID, subscriber)
		return nil, errors.New("pending subscriber could not start")
	}
	go func() {
		<-ctx.Done()
		b.unsubscribe(sessionID, subscriber)
	}()
	return subscriber.updates, nil
}

func (b *pendingBroker) unsubscribe(sessionID domain.SessionID, subscriber *pendingSubscriber) {
	b.mu.Lock()
	if subscribers := b.subscribers[sessionID]; subscribers != nil {
		delete(subscribers, subscriber)
		if len(subscribers) == 0 {
			delete(b.subscribers, sessionID)
		}
	}
	b.mu.Unlock()
	subscriber.close()
}

func (b *pendingBroker) publish(request RequestDTO) {
	b.mu.Lock()
	targets := make([]*pendingSubscriber, 0, len(b.subscribers[request.SessionID]))
	for subscriber := range b.subscribers[request.SessionID] {
		targets = append(targets, subscriber)
	}
	b.mu.Unlock()

	for _, target := range targets {
		target.send(request)
	}
}

type pendingSubscriber struct {
	mu      sync.Mutex
	updates chan RequestDTO
	wake    chan struct{}
	done    chan struct{}
	queue   []RequestDTO
	started bool
	closed  bool
}

func newPendingSubscriber() *pendingSubscriber {
	return &pendingSubscriber{
		updates: make(chan RequestDTO),
		wake:    make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
}

func (s *pendingSubscriber) start() bool {
	s.mu.Lock()
	if s.closed || s.started {
		s.mu.Unlock()
		return false
	}
	s.started = true
	s.mu.Unlock()
	go s.run()
	return true
}

func (s *pendingSubscriber) send(request RequestDTO) bool {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false
	}
	s.queue = append(s.queue, request)
	s.mu.Unlock()

	select {
	case s.wake <- struct{}{}:
	default:
	}
	return true
}

func (s *pendingSubscriber) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	if s.started {
		close(s.done)
	} else {
		close(s.updates)
	}
	s.mu.Unlock()
}

func (s *pendingSubscriber) run() {
	defer close(s.updates)
	for {
		request, ok := s.next()
		if ok {
			select {
			case s.updates <- request:
			case <-s.done:
				return
			}
			continue
		}
		select {
		case <-s.wake:
		case <-s.done:
			return
		}
	}
}

func (s *pendingSubscriber) next() (RequestDTO, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return RequestDTO{}, false
	}
	request := s.queue[0]
	s.queue[0] = RequestDTO{}
	s.queue = s.queue[1:]
	return request, true
}
