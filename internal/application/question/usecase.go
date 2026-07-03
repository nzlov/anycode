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
	Wait(ctx context.Context, id domain.BatchID) ([]domain.Answer, error)
	SubmitBatch(ctx context.Context, input SubmitBatchInput) (BatchDTO, error)
	GetBatch(ctx context.Context, id domain.BatchID) (BatchDTO, error)
	ListPendingBySession(ctx context.Context, sessionID domain.SessionID) ([]BatchDTO, error)
	PendingQuestionBatches(ctx context.Context, sessionID domain.SessionID) (<-chan BatchDTO, error)
	CancelPendingBySession(ctx context.Context, sessionID domain.SessionID, reason string) error
}

type CreateBatchInput struct {
	SessionID     domain.SessionID
	WorkflowRunID *domain.WorkflowRunID
	Questions     []domain.Question
}

type SubmitBatchInput struct {
	BatchID domain.BatchID
	Answers []domain.Answer
}

type BatchDTO struct {
	ID            domain.BatchID
	SessionID     domain.SessionID
	WorkflowRunID *domain.WorkflowRunID
	Status        domain.BatchStatus
	Questions     []domain.Question
}

type Service struct {
	repo       domain.Repository
	policy     domain.Policy
	waiter     domain.AnswerWaiter
	now        func() time.Time
	generateID func() (string, error)
	broker     *pendingBroker
}

func New(repo domain.Repository, waiter domain.AnswerWaiter) *Service {
	return &Service{
		repo:       repo,
		policy:     domain.DefaultPolicy{},
		waiter:     waiter,
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
		ID:            domain.BatchID(batchID),
		SessionID:     input.SessionID,
		WorkflowRunID: input.WorkflowRunID,
		Status:        domain.BatchPending,
		Questions:     questions,
		CreatedAt:     s.now(),
	}
	if err := s.repo.CreateBatch(ctx, batch); err != nil {
		return BatchDTO{}, fmt.Errorf("create question batch: %w", err)
	}
	dto := toDTO(batch)
	s.publish(dto)
	return dto, nil
}

func (s *Service) Wait(ctx context.Context, id domain.BatchID) ([]domain.Answer, error) {
	if s == nil {
		return nil, errors.New("question usecase: nil service")
	}
	if s.waiter == nil {
		return nil, errors.New("question answer waiter is required")
	}
	answers, err := s.waiter.Wait(ctx, id)
	if err != nil {
		if errors.Is(err, ErrWaitCancelled) {
			return nil, apperror.Wrap(err, apperror.CodeAnswerUserCancelled, apperror.CategoryUserActionRequired, "answer_user wait was cancelled")
		}
		return nil, fmt.Errorf("wait for question answers: %w", err)
	}
	return answers, nil
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
	answered, err := s.policy.ApplyAnswers(batch, input.Answers)
	if err != nil {
		return BatchDTO{}, apperror.Wrap(err, apperror.CodeValidationFailed, apperror.CategoryValidationError, "question answers are invalid").WithDetails(map[string]any{"batchId": string(input.BatchID)})
	}
	if err := s.repo.SubmitAnswers(ctx, input.BatchID, input.Answers); err != nil {
		return BatchDTO{}, fmt.Errorf("submit question answers: %w", err)
	}
	if s.waiter != nil {
		if err := s.waiter.Resume(ctx, input.BatchID, input.Answers); err != nil {
			return BatchDTO{}, fmt.Errorf("resume question waiter: %w", err)
		}
	}
	dto := toDTO(answered)
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
		if len(answer.Payload) > 0 && len(question.Answer) > 0 && !reflect.DeepEqual(question.Answer, answer.Payload) {
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

func (s *Service) PendingQuestionBatches(ctx context.Context, sessionID domain.SessionID) (<-chan BatchDTO, error) {
	pending, err := s.ListPendingBySession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	out := make(chan BatchDTO, len(pending)+1)
	for _, batch := range pending {
		out <- batch
	}
	if s.broker == nil {
		close(out)
		return out, nil
	}
	s.broker.subscribe(ctx, sessionID, out)
	return out, nil
}

func (s *Service) CancelPendingBySession(ctx context.Context, sessionID domain.SessionID, reason string) error {
	if s == nil {
		return errors.New("question usecase: nil service")
	}
	if s.repo == nil {
		return errors.New("question repository is required")
	}
	if s.policy == nil {
		return errors.New("question policy is required")
	}
	pending, err := s.repo.ListPendingBySession(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("list pending question batches: %w", err)
	}
	if err := s.repo.CancelPendingBySession(ctx, sessionID, reason); err != nil {
		return fmt.Errorf("cancel pending question batches: %w", err)
	}
	for _, batch := range pending {
		cancelled, err := s.policy.Cancel(batch, reason)
		if err != nil {
			return err
		}
		s.publish(toDTO(cancelled))
		if s.waiter != nil {
			if err := s.waiter.Cancel(ctx, batch.ID, reason); err != nil {
				return fmt.Errorf("cancel question waiter: %w", err)
			}
		}
	}
	return nil
}

func toDTO(batch domain.Batch) BatchDTO {
	return BatchDTO{
		ID:            batch.ID,
		SessionID:     batch.SessionID,
		WorkflowRunID: batch.WorkflowRunID,
		Status:        batch.Status,
		Questions:     append([]domain.Question(nil), batch.Questions...),
	}
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
	subscribers map[domain.SessionID]map[chan BatchDTO]struct{}
}

func newPendingBroker() *pendingBroker {
	return &pendingBroker{subscribers: map[domain.SessionID]map[chan BatchDTO]struct{}{}}
}

func (b *pendingBroker) subscribe(ctx context.Context, sessionID domain.SessionID, out chan BatchDTO) {
	b.mu.Lock()
	if b.subscribers == nil {
		b.subscribers = map[domain.SessionID]map[chan BatchDTO]struct{}{}
	}
	if b.subscribers[sessionID] == nil {
		b.subscribers[sessionID] = map[chan BatchDTO]struct{}{}
	}
	b.subscribers[sessionID][out] = struct{}{}
	b.mu.Unlock()

	go func() {
		<-ctx.Done()
		b.mu.Lock()
		if subscribers := b.subscribers[sessionID]; subscribers != nil {
			delete(subscribers, out)
			if len(subscribers) == 0 {
				delete(b.subscribers, sessionID)
			}
		}
		b.mu.Unlock()
		close(out)
	}()
}

func (b *pendingBroker) publish(batch BatchDTO) {
	b.mu.Lock()
	targets := make([]chan BatchDTO, 0, len(b.subscribers[batch.SessionID]))
	for subscriber := range b.subscribers[batch.SessionID] {
		targets = append(targets, subscriber)
	}
	b.mu.Unlock()

	for _, target := range targets {
		select {
		case target <- batch:
		default:
		}
	}
}
