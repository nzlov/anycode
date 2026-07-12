package graph

import (
	"context"

	eventapp "github.com/nzlov/anycode/internal/application/event"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph/model"
)

func (r *subscriptionResolver) sessionCardChanges(ctx context.Context, projectID *string) (<-chan *model.SessionCard, error) {
	if r.UseCases.Events == nil {
		return nil, missingUseCase("events")
	}
	if r.UseCases.Sessions == nil {
		return nil, missingUseCase("sessions")
	}
	scope := eventdomain.Scope{}
	if projectID != nil {
		scope.ProjectID = *projectID
	}
	source, err := r.UseCases.Events.LiveSessionEvents(ctx, eventapp.LiveSessionEventsInput{Scope: scope})
	if err != nil {
		return nil, err
	}
	out := make(chan *model.SessionCard)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case eventDTO, ok := <-source:
				if !ok {
					return
				}
				if !sessionCardChangeEvent(eventDTO, projectID) || eventDTO.SessionID == nil {
					continue
				}
				card, err := r.UseCases.Sessions.GetSessionCard(ctx, sessiondomain.ID(*eventDTO.SessionID))
				if err != nil {
					continue
				}
				select {
				case out <- mapSessionCard(card):
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}
