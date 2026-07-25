package tunnelevent

import (
	"context"
	"errors"

	eventapp "github.com/nzlov/anycode/internal/application/event"
	tunnelapp "github.com/nzlov/anycode/internal/application/tunnel"
	tunneldomain "github.com/nzlov/anycode/internal/domain/tunnel"
)

const (
	TypeCountSnapshot = "tunnel.count_snapshot"
	TypeCountUpdated  = "tunnel.count_updated"
)

type DTO struct {
	Type         string
	RunningCount int
}

type UseCase interface {
	TunnelUpdates(ctx context.Context) (<-chan DTO, error)
}

type EventSource interface {
	// GLUE: the shared domain-event hub retains its legacy session-oriented method name.
	LiveSessionEvents(ctx context.Context, input eventapp.LiveSessionEventsInput) (<-chan eventapp.DTO, error)
}

type TunnelSource interface {
	List(ctx context.Context) ([]tunnelapp.DTO, error)
}

type Service struct {
	events  EventSource
	tunnels TunnelSource
}

func New(events EventSource, tunnels TunnelSource) *Service {
	return &Service{events: events, tunnels: tunnels}
}

func (s *Service) TunnelUpdates(ctx context.Context) (<-chan DTO, error) {
	if s == nil || s.events == nil || s.tunnels == nil {
		return nil, errors.New("tunnel update usecase is not fully configured")
	}
	events, err := s.events.LiveSessionEvents(ctx, eventapp.LiveSessionEventsInput{})
	if err != nil {
		return nil, err
	}
	items, err := s.tunnels.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make(chan DTO)
	go func() {
		defer close(out)
		if !send(ctx, out, DTO{Type: TypeCountSnapshot, RunningCount: runningCount(items)}) {
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case event, ok := <-events:
				if !ok {
					return
				}
				if event.Type != TypeCountUpdated {
					continue
				}
				count, ok := event.Payload["runningCount"].(int)
				if ok && !send(ctx, out, DTO{Type: event.Type, RunningCount: count}) {
					return
				}
			}
		}
	}()
	return out, nil
}

func runningCount(items []tunnelapp.DTO) int {
	count := 0
	for _, item := range items {
		if item.Status == tunneldomain.StatusRunning {
			count++
		}
	}
	return count
}

func send(ctx context.Context, out chan<- DTO, event DTO) bool {
	select {
	case <-ctx.Done():
		return false
	case out <- event:
		return true
	}
}
