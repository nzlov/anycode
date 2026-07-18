package sessionevent

import (
	"context"
	"errors"

	eventapp "github.com/nzlov/anycode/internal/application/event"
	questionapp "github.com/nzlov/anycode/internal/application/question"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	timelineapp "github.com/nzlov/anycode/internal/application/timeline"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	questiondomain "github.com/nzlov/anycode/internal/domain/question"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
)

const (
	TypeUsage    = "usage.updated"
	TypeQuestion = "question.updated"
)

type DTO struct {
	ID            string
	Type          string
	OccurredAt    string
	Transcript    *timelineapp.DTO
	Usage         *timelineapp.TokenUsageDTO
	Session       *sessionapp.DetailDTO
	QuestionBatch *questionapp.BatchDTO
}

type UseCase interface {
	SessionEvents(ctx context.Context, sessionID sessiondomain.ID) (<-chan DTO, error)
}

type TimelineSource interface {
	SessionEvents(ctx context.Context, input timelineapp.SessionEventsInput) (<-chan timelineapp.DTO, error)
}

type DomainEventSource interface {
	LiveSessionEvents(ctx context.Context, input eventapp.LiveSessionEventsInput) (<-chan eventapp.DTO, error)
}

type SessionSource interface {
	GetSession(ctx context.Context, id sessiondomain.ID) (sessionapp.DetailDTO, error)
}

type QuestionSource interface {
	QuestionBatchUpdates(ctx context.Context, sessionID questiondomain.SessionID) (<-chan questionapp.BatchDTO, error)
}

type Service struct {
	timeline  TimelineSource
	events    DomainEventSource
	sessions  SessionSource
	questions QuestionSource
}

func New(timeline TimelineSource, events DomainEventSource, sessions SessionSource, questions QuestionSource) *Service {
	return &Service{timeline: timeline, events: events, sessions: sessions, questions: questions}
}

func (s *Service) SessionEvents(ctx context.Context, sessionID sessiondomain.ID) (<-chan DTO, error) {
	if s == nil || s.timeline == nil || s.events == nil || s.sessions == nil || s.questions == nil {
		return nil, errors.New("session event usecase is not fully configured")
	}
	if sessionID == "" {
		return nil, errors.New("session id is required")
	}
	streamCtx, cancel := context.WithCancel(ctx)
	transcript, err := s.timeline.SessionEvents(streamCtx, timelineapp.SessionEventsInput{SessionID: sessionID})
	if err != nil {
		cancel()
		return nil, err
	}
	eventSessionID := eventdomain.SessionID(sessionID)
	domainEvents, err := s.events.LiveSessionEvents(streamCtx, eventapp.LiveSessionEventsInput{
		Scope: eventdomain.Scope{SessionID: &eventSessionID},
	})
	if err != nil {
		cancel()
		return nil, err
	}
	questions, err := s.questions.QuestionBatchUpdates(streamCtx, questiondomain.SessionID(sessionID))
	if err != nil {
		cancel()
		return nil, err
	}
	out := make(chan DTO, 16)
	go func() {
		defer close(out)
		defer cancel()
		for {
			select {
			case <-streamCtx.Done():
				return
			case item, ok := <-transcript:
				if !ok {
					return
				}
				event := DTO{ID: string(item.ID), Type: string(item.Type), OccurredAt: item.OccurredAt}
				if item.Usage != nil {
					event.Type = TypeUsage
					event.Usage = item.Usage
				} else {
					event.Transcript = &item
				}
				if !send(streamCtx, out, event) {
					return
				}
			case item, ok := <-domainEvents:
				if !ok {
					return
				}
				event := DTO{ID: string(item.ID), Type: item.Type, OccurredAt: item.CreatedAt}
				session, err := s.sessions.GetSession(streamCtx, sessionID)
				if err != nil {
					return
				}
				event.Session = &session
				if !send(streamCtx, out, event) {
					return
				}
			case batch, ok := <-questions:
				if !ok {
					return
				}
				event := DTO{ID: string(batch.ID), Type: TypeQuestion, QuestionBatch: &batch}
				session, err := s.sessions.GetSession(streamCtx, sessionID)
				if err != nil {
					return
				}
				event.Session = &session
				if !send(streamCtx, out, event) {
					return
				}
			}
		}
	}()
	return out, nil
}

func send(ctx context.Context, out chan<- DTO, event DTO) bool {
	select {
	case <-ctx.Done():
		return false
	case out <- event:
		return true
	}
}
