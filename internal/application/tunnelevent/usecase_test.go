package tunnelevent

import (
	"context"
	"testing"

	eventapp "github.com/nzlov/anycode/internal/application/event"
	tunnelapp "github.com/nzlov/anycode/internal/application/tunnel"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	tunneldomain "github.com/nzlov/anycode/internal/domain/tunnel"
)

func TestTunnelUpdatesStartsWithCurrentCountAndForwardsCountEvents(t *testing.T) {
	events := eventapp.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := New(events, tunnelSourceStub{items: []tunnelapp.DTO{
		{Status: tunneldomain.StatusRunning},
		{Status: tunneldomain.StatusStarting},
	}})

	updates, err := service.TunnelUpdates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if initial := <-updates; initial.Type != TypeCountSnapshot || initial.RunningCount != 1 {
		t.Fatalf("initial update = %#v", initial)
	}
	if err := events.PublishAfterCommit(ctx, eventdomain.DomainEvent{
		Type: TypeCountUpdated, Payload: map[string]any{"runningCount": 3},
	}); err != nil {
		t.Fatal(err)
	}
	if update := <-updates; update.Type != TypeCountUpdated || update.RunningCount != 3 {
		t.Fatalf("count update = %#v", update)
	}
}

type tunnelSourceStub struct {
	items []tunnelapp.DTO
}

func (s tunnelSourceStub) List(context.Context) ([]tunnelapp.DTO, error) { return s.items, nil }
