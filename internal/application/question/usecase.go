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
	CreateBatch(ctx context.Context, input CreateBatchInput) (BatchDTO, error)
	SubmitBatch(ctx context.Context, input SubmitBatchInput) (BatchDTO, error)
	GetBatch(ctx context.Context, id domain.BatchID) (BatchDTO, error)
	ListPendingBySession(ctx context.Context, sessionID domain.SessionID) ([]BatchDTO, error)
	QuestionBatchUpdates(ctx context.Context, sessionID domain.SessionID) (<-chan BatchDTO, error)
	CancelPendingBySession(ctx context.Context, sessionID domain.SessionID, reason string) error
}

type CreateBatchInput struct {
	SessionID          domain.SessionID
	WorkflowRunID      *domain.WorkflowRunID
	OriginProcessRunID *domain.ProcessRunID
	Questions          []domain.Question
}

type SubmitBatchInput struct {
	BatchID domain.BatchID
	Answers []domain.Answer
}

type BatchDTO struct {
	ID                   domain.BatchID
	SessionID            domain.SessionID
	WorkflowRunID        *domain.WorkflowRunID
	OriginProcessRunID   *domain.ProcessRunID
	Status               domain.BatchStatus
	DeliveryStatus       domain.DeliveryStatus
	DeliveryProcessRunID *domain.ProcessRunID
	Questions            []domain.Question
}

type Service struct {
	repo       domain.Repository
	policy     domain.Policy
	now        func() time.Time
	generateID func() (string, error)
	broker     *pendingBroker
}

func New(repo domain.Repository) *Service {
	return &Service{
		repo:       repo,
		policy:     domain.DefaultPolicy{},
		now:        time.Now,
		generateID: generateID,
		broker:     newPendingBroker(),
	}
}

func (s *Service) CreateBatch(ctx context.Context, input CreateBatchInput) (BatchDTO, error) {
	if s == nil {
		return BatchDTO{}, errors.New("question usecase: nil service")
	}
	if s.repo == nil {
		return BatchDTO{}, errors.New("question repository is required")
	}
	if input.SessionID == "" {
		return BatchDTO{}, errors.New("session id is required")
	}
	if len(input.Questions) == 0 {
		return BatchDTO{}, errors.New("questions are required")
	}
	batchID, err := s.generateID()
	if err != nil {
		return BatchDTO{}, fmt.Errorf("generate question batch id: %w", err)
	}
	questions := make([]domain.Question, len(input.Questions))
	for i, item := range input.Questions {
		questions[i] = item
		if questions[i].ID == "" {
			id, err := s.generateID()
			if err != nil {
				return BatchDTO{}, fmt.Errorf("generate question id: %w", err)
			}
			questions[i].ID = domain.QuestionID(id)
		}
		questions[i].BatchID = domain.BatchID(batchID)
	}
	batch := domain.Batch{
		ID:                 domain.BatchID(batchID),
		SessionID:          input.SessionID,
		WorkflowRunID:      input.WorkflowRunID,
		OriginProcessRunID: input.OriginProcessRunID,
		Status:             domain.BatchPending,
		DeliveryStatus:     domain.DeliveryNone,
		Questions:          questions,
		CreatedAt:          s.now(),
	}
	if err := s.repo.CreateBatch(ctx, batch); err != nil {
		return BatchDTO{}, fmt.Errorf("create question batch: %w", err)
	}
	dto := toDTO(batch)
	s.publish(dto)
	return dto, nil
}

func (s *Service) SubmitBatch(ctx context.Context, input SubmitBatchInput) (BatchDTO, error) {
	if s == nil {
		return BatchDTO{}, errors.New("question usecase: nil service")
	}
	if s.repo == nil {
		return BatchDTO{}, errors.New("question repository is required")
	}
	if s.policy == nil {
		return BatchDTO{}, errors.New("question policy is required")
	}
	batch, err := s.repo.FindBatch(ctx, input.BatchID)
	if err != nil {
		return BatchDTO{}, apperror.Wrap(err, apperror.CodeNotFound, apperror.CategoryValidationError, "question batch not found").WithDetails(map[string]any{"batchId": string(input.BatchID)})
	}
	if batch.Status == domain.BatchAnswered {
		if err := ensureAnswersMatchAnsweredBatch(batch, input.Answers); err != nil {
			return BatchDTO{}, apperror.Wrap(err, apperror.CodeValidationFailed, apperror.CategoryValidationError, "question answers do not match existing answers")
		}
		return toDTO(batch), nil
	}
	if err := s.policy.CanSubmit(batch, input.Answers); err != nil {
		return BatchDTO{}, apperror.Wrap(err, apperror.CodeValidationFailed, apperror.CategoryValidationError, "question answers are invalid").WithDetails(map[string]any{"batchId": string(input.BatchID)})
	}
	persisted, transitioned, err := s.repo.SubmitAnswers(ctx, input.BatchID, input.Answers)
	if err != nil {
		return BatchDTO{}, fmt.Errorf("submit question answers: %w", err)
	}
	if !transitioned {
		if persisted.Status == domain.BatchAnswered {
			if err := ensureAnswersMatchAnsweredBatch(persisted, input.Answers); err != nil {
				return BatchDTO{}, apperror.Wrap(err, apperror.CodeValidationFailed, apperror.CategoryValidationError, "question answers do not match existing answers")
			}
			return toDTO(persisted), nil
		}
		return BatchDTO{}, apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "question batch is no longer pending").WithDetails(map[string]any{"batchId": string(input.BatchID), "status": string(persisted.Status)})
	}
	dto := toDTO(persisted)
	s.publish(dto)
	return dto, nil
}

func ensureAnswersMatchAnsweredBatch(batch domain.Batch, answers []domain.Answer) error {
	if len(answers) != len(batch.Questions) {
		return fmt.Errorf("question batch %s is already answered with different answers", batch.ID)
	}
	byQuestion := make(map[domain.QuestionID]domain.Answer, len(answers))
	for _, answer := range answers {
		if _, exists := byQuestion[answer.QuestionID]; exists {
			return fmt.Errorf("question %s has duplicate answers", answer.QuestionID)
		}
		byQuestion[answer.QuestionID] = answer
	}
	for _, question := range batch.Questions {
		answer, ok := byQuestion[question.ID]
		if !ok {
			return fmt.Errorf("question batch %s is already answered with different answers", batch.ID)
		}
		if !sameOptionID(question.SelectedOptionID, answer.SelectedOptionID) {
			return fmt.Errorf("question batch %s is already answered with different answers", batch.ID)
		}
		if strings.TrimSpace(question.CustomAnswer) != strings.TrimSpace(answer.CustomAnswer) {
			return fmt.Errorf("question batch %s is already answered with different answers", batch.ID)
		}
		if (len(answer.Payload) > 0 || len(question.Answer) > 0) && !reflect.DeepEqual(question.Answer, answer.Payload) {
			return fmt.Errorf("question batch %s is already answered with different answers", batch.ID)
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

func (s *Service) GetBatch(ctx context.Context, id domain.BatchID) (BatchDTO, error) {
	if s == nil {
		return BatchDTO{}, errors.New("question usecase: nil service")
	}
	if s.repo == nil {
		return BatchDTO{}, errors.New("question repository is required")
	}
	batch, err := s.repo.FindBatch(ctx, id)
	if err != nil {
		return BatchDTO{}, apperror.Wrap(err, apperror.CodeNotFound, apperror.CategoryValidationError, "question batch not found").WithDetails(map[string]any{"batchId": string(id)})
	}
	return toDTO(batch), nil
}

func (s *Service) ListPendingBySession(ctx context.Context, sessionID domain.SessionID) ([]BatchDTO, error) {
	if s == nil {
		return nil, errors.New("question usecase: nil service")
	}
	if s.repo == nil {
		return nil, errors.New("question repository is required")
	}
	batches, err := s.repo.ListPendingBySession(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list pending question batches: %w", err)
	}
	dtos := make([]BatchDTO, 0, len(batches))
	for _, batch := range batches {
		dtos = append(dtos, toDTO(batch))
	}
	return dtos, nil
}

func (s *Service) QuestionBatchUpdates(ctx context.Context, sessionID domain.SessionID) (<-chan BatchDTO, error) {
	if s == nil || s.broker == nil {
		return nil, errors.New("question update broker is required")
	}
	return s.broker.subscribe(ctx, sessionID)
}

func (s *Service) CancelPendingBySession(ctx context.Context, sessionID domain.SessionID, reason string) error {
	if s == nil {
		return errors.New("question usecase: nil service")
	}
	if s.repo == nil {
		return errors.New("question repository is required")
	}
	cancelled, err := s.repo.CancelPendingBySession(ctx, sessionID, reason)
	if err != nil {
		return fmt.Errorf("cancel pending question batches: %w", err)
	}
	for _, batch := range cancelled {
		s.publish(toDTO(batch))
	}
	if repo, ok := s.repo.(domain.AgentRepository); ok {
		undelivered, err := repo.CancelUndeliveredBySession(ctx, sessionID)
		if err != nil {
			return fmt.Errorf("cancel undelivered question answers: %w", err)
		}
		for _, batch := range undelivered {
			s.publish(toDTO(batch))
		}
	}
	return nil
}

func toDTO(batch domain.Batch) BatchDTO {
	return BatchDTO{
		ID:                   batch.ID,
		SessionID:            batch.SessionID,
		WorkflowRunID:        batch.WorkflowRunID,
		OriginProcessRunID:   batch.OriginProcessRunID,
		Status:               batch.Status,
		DeliveryStatus:       batch.DeliveryStatus,
		DeliveryProcessRunID: batch.DeliveryProcessRunID,
		Questions:            append([]domain.Question(nil), batch.Questions...),
	}
}

func (s *Service) PublishBatch(batch BatchDTO) {
	s.publish(batch)
}

func generateID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func (s *Service) publish(batch BatchDTO) {
	if s != nil && s.broker != nil {
		s.broker.publish(batch)
	}
}

type pendingBroker struct {
	mu          sync.Mutex
	subscribers map[domain.SessionID]map[*pendingSubscriber]struct{}
}

func newPendingBroker() *pendingBroker {
	return &pendingBroker{subscribers: map[domain.SessionID]map[*pendingSubscriber]struct{}{}}
}

func (b *pendingBroker) subscribe(ctx context.Context, sessionID domain.SessionID) (<-chan BatchDTO, error) {
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

func (b *pendingBroker) publish(batch BatchDTO) {
	b.mu.Lock()
	targets := make([]*pendingSubscriber, 0, len(b.subscribers[batch.SessionID]))
	for subscriber := range b.subscribers[batch.SessionID] {
		targets = append(targets, subscriber)
	}
	b.mu.Unlock()

	for _, target := range targets {
		target.send(batch)
	}
}

type pendingSubscriber struct {
	mu      sync.Mutex
	updates chan BatchDTO
	wake    chan struct{}
	done    chan struct{}
	queue   []BatchDTO
	started bool
	closed  bool
}

func newPendingSubscriber() *pendingSubscriber {
	return &pendingSubscriber{
		updates: make(chan BatchDTO),
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

func (s *pendingSubscriber) send(batch BatchDTO) bool {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false
	}
	s.queue = append(s.queue, batch)
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
		batch, ok := s.next()
		if ok {
			select {
			case s.updates <- batch:
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

func (s *pendingSubscriber) next() (BatchDTO, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return BatchDTO{}, false
	}
	batch := s.queue[0]
	s.queue[0] = BatchDTO{}
	s.queue = s.queue[1:]
	return batch, true
}
