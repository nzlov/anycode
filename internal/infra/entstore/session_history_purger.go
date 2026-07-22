package entstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/nzlov/anycode/internal/application/port"
	"github.com/nzlov/anycode/internal/domain/session"
	enteventrecord "github.com/nzlov/anycode/internal/infra/entstore/ent/eventrecord"
	entmergerecord "github.com/nzlov/anycode/internal/infra/entstore/ent/mergerecord"
	entnoderun "github.com/nzlov/anycode/internal/infra/entstore/ent/noderun"
	entnotificationdelivery "github.com/nzlov/anycode/internal/infra/entstore/ent/notificationdelivery"
	entprocessrun "github.com/nzlov/anycode/internal/infra/entstore/ent/processrun"
	entpromptappend "github.com/nzlov/anycode/internal/infra/entstore/ent/promptappend"
	entquestionbatch "github.com/nzlov/anycode/internal/infra/entstore/ent/questionbatch"
	entsession "github.com/nzlov/anycode/internal/infra/entstore/ent/session"
)

var _ port.SessionHistoryPurger = (*Store)(nil)

func (s *Store) PurgeSessions(ctx context.Context, ids []session.ID) error {
	if s == nil || s.client == nil {
		return errors.New("entstore: nil store")
	}
	stringIDs := uniqueSessionIDs(ids)
	if len(stringIDs) == 0 {
		return nil
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin session history purge: %w", err)
	}
	client := tx.Client()
	events, err := client.EventRecord.Query().
		Where(enteventrecord.SessionIDIn(stringIDs...)).
		Select(enteventrecord.FieldID).
		All(ctx)
	if err != nil {
		err = fmt.Errorf("list session event records: %w", err)
	}
	if err == nil && len(events) > 0 {
		eventIDs := make([]string, 0, len(events))
		for _, event := range events {
			eventIDs = append(eventIDs, event.ID)
		}
		if _, deleteErr := client.NotificationDelivery.Delete().
			Where(entnotificationdelivery.EventIDIn(eventIDs...)).
			Exec(ctx); deleteErr != nil {
			err = fmt.Errorf("delete notification deliveries: %w", deleteErr)
		}
	}
	if err == nil {
		if _, deleteErr := client.EventRecord.Delete().Where(enteventrecord.SessionIDIn(stringIDs...)).Exec(ctx); deleteErr != nil {
			err = fmt.Errorf("delete event records: %w", deleteErr)
		}
	}
	if err == nil {
		if _, deleteErr := client.QuestionBatch.Delete().Where(entquestionbatch.SessionIDIn(stringIDs...)).Exec(ctx); deleteErr != nil {
			err = fmt.Errorf("delete question batches: %w", deleteErr)
		}
	}
	if err == nil {
		if _, deleteErr := client.NodeRun.Delete().Where(entnoderun.SessionIDIn(stringIDs...)).Exec(ctx); deleteErr != nil {
			err = fmt.Errorf("delete node runs: %w", deleteErr)
		}
	}
	if err == nil {
		if _, deleteErr := client.ProcessRun.Delete().Where(entprocessrun.SessionIDIn(stringIDs...)).Exec(ctx); deleteErr != nil {
			err = fmt.Errorf("delete process runs: %w", deleteErr)
		}
	}
	if err == nil {
		if _, deleteErr := client.PromptAppend.Delete().Where(entpromptappend.SessionIDIn(stringIDs...)).Exec(ctx); deleteErr != nil {
			err = fmt.Errorf("delete prompt appends: %w", deleteErr)
		}
	}
	if err == nil {
		if _, deleteErr := client.MergeRecord.Delete().Where(entmergerecord.SessionIDIn(stringIDs...)).Exec(ctx); deleteErr != nil {
			err = fmt.Errorf("delete merge records: %w", deleteErr)
		}
	}
	if err == nil {
		if _, deleteErr := client.Session.Delete().Where(entsession.IDIn(stringIDs...)).Exec(ctx); deleteErr != nil {
			err = fmt.Errorf("delete sessions: %w", deleteErr)
		}
	}
	if err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("purge session history: %w; rollback: %v", err, rollbackErr)
		}
		return fmt.Errorf("purge session history: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit session history purge: %w", err)
	}
	return nil
}

func uniqueSessionIDs(ids []session.ID) []string {
	seen := make(map[session.ID]struct{}, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, string(id))
	}
	return result
}
