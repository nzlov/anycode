package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	processdomain "github.com/nzlov/anycode/internal/domain/process"
	domain "github.com/nzlov/anycode/internal/domain/session"
)

const sidePersistenceTimeout = 10 * time.Second

type sideRun struct {
	mu          sync.Mutex
	nextID      uint64
	subscribers map[uint64]chan processdomain.CodexEvent
	closed      bool
	done        chan struct{}
}

func newSideRun() *sideRun {
	return &sideRun{subscribers: map[uint64]chan processdomain.CodexEvent{}, done: make(chan struct{})}
}

func (r *sideRun) subscribe() (<-chan processdomain.CodexEvent, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ch := make(chan processdomain.CodexEvent, 1024)
	if r.closed {
		close(ch)
		return ch, func() {}
	}
	r.nextID++
	id := r.nextID
	r.subscribers[id] = ch
	return ch, func() {
		r.mu.Lock()
		if current, ok := r.subscribers[id]; ok {
			delete(r.subscribers, id)
			close(current)
		}
		r.mu.Unlock()
	}
}

func (r *sideRun) publish(event processdomain.CodexEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	for _, subscriber := range r.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func (r *sideRun) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	for id, subscriber := range r.subscribers {
		delete(r.subscribers, id)
		close(subscriber)
	}
	close(r.done)
}

func (s *Service) claimSideRun(ctx context.Context, handle processdomain.CodexHandle) (<-chan processdomain.CodexEvent, *sideRun, error) {
	ephemeral, ok := s.codex.(processdomain.CodexEphemeralThread)
	if !ok {
		return nil, nil, errors.New("Codex runtime does not support temporary Side questions")
	}
	source, err := ephemeral.EphemeralEvents(ctx, handle.ProcessRunID)
	if err != nil {
		return nil, nil, fmt.Errorf("subscribe temporary Side events: %w", err)
	}
	run := newSideRun()
	if _, loaded := s.sideRuns.LoadOrStore(handle.ProcessRunID, run); loaded {
		return nil, nil, errors.New("temporary Side run is already registered")
	}
	return source, run, nil
}

func (s *Service) discardSideRun(processRunID processdomain.RunID) {
	value, ok := s.sideRuns.LoadAndDelete(processRunID)
	if ok {
		value.(*sideRun).close()
	}
}

func (s *Service) collectSideRun(side domain.Side, source <-chan processdomain.CodexEvent, run *sideRun) {
	s.sideWG.Add(1)
	go func() {
		defer s.sideWG.Done()
		defer func() {
			run.close()
			s.sideRuns.CompareAndDelete(processdomain.RunID(side.ProcessRunID), run)
		}()
		for {
			select {
			case <-s.lifecycleCtx.Done():
				return
			case event, ok := <-source:
				if !ok {
					s.completeSideRun(side, domain.SideStatusFailed, "Side 事件流意外结束")
					return
				}
				if event.Type == processdomain.CodexEventProcessExit {
					status, failure := sideExitStatus(event.Content)
					s.completeSideRun(side, status, failure)
					return
				}
				event = processdomain.PrepareCodexEventForTranscript(event, false)
				stored, visible, err := encodeSideEvent(side, event)
				if err != nil {
					s.failSideCollector(side, err)
					return
				}
				if !visible {
					continue
				}
				persistCtx, cancel := context.WithTimeout(context.Background(), sidePersistenceTimeout)
				err = s.sides.AppendSideEvent(persistCtx, stored)
				cancel()
				if err != nil {
					s.failSideCollector(side, err)
					return
				}
				run.publish(event)
			}
		}
	}()
}

func (s *Service) failSideCollector(side domain.Side, cause error) {
	log.Printf("persist Side event: session=%s side=%s run=%s error=%v", side.SessionID, side.ID, side.ProcessRunID, cause)
	s.completeSideRun(side, domain.SideStatusFailed, "保存 Side 记录失败")
	ctx, cancel := context.WithTimeout(context.Background(), sidePersistenceTimeout)
	defer cancel()
	if err := s.stopEphemeralSide(ctx, processdomain.RunID(side.ProcessRunID)); err != nil {
		log.Printf("stop Side after persistence failure: side=%s run=%s error=%v", side.ID, side.ProcessRunID, err)
	}
}

func (s *Service) completeSideRun(side domain.Side, status domain.SideStatus, failure string) {
	ctx, cancel := context.WithTimeout(context.Background(), sidePersistenceTimeout)
	defer cancel()
	if err := s.sides.CompleteSideTurn(ctx, side.ID, side.ProcessRunID, status, failure, s.now()); err != nil {
		log.Printf("complete Side turn: session=%s side=%s run=%s error=%v", side.SessionID, side.ID, side.ProcessRunID, err)
	}
}

func sideExitStatus(content processdomain.CodexEventContent) (domain.SideStatus, string) {
	result, ok := content.(processdomain.ExitResult)
	if !ok {
		return domain.SideStatusFailed, "Side 运行异常结束"
	}
	if result.FailureCode == "" && strings.TrimSpace(result.FailureReason) == "" && (result.ExitCode == nil || *result.ExitCode == 0) {
		return domain.SideStatusCompleted, ""
	}
	failure := strings.TrimSpace(result.FailureReason)
	if failure == "" {
		failure = "Side 运行已中断"
	}
	return domain.SideStatusFailed, failure
}

func (s *Service) hasActiveSideRun(processRunID processdomain.RunID) bool {
	_, ok := s.sideRuns.Load(processRunID)
	return ok
}

func (s *Service) subscribeSideRun(processRunID processdomain.RunID) (<-chan processdomain.CodexEvent, func()) {
	value, ok := s.sideRuns.Load(processRunID)
	if !ok {
		ch := make(chan processdomain.CodexEvent)
		close(ch)
		return ch, func() {}
	}
	return value.(*sideRun).subscribe()
}

func (s *Service) waitForSideRun(ctx context.Context, processRunID processdomain.RunID) error {
	value, ok := s.sideRuns.Load(processRunID)
	if !ok {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-value.(*sideRun).done:
		return nil
	}
}

func (s *Service) recoverInterruptedSides(ctx context.Context) error {
	if s.sides == nil {
		return nil
	}
	items, err := s.sides.ListRunningSides(ctx)
	if err != nil {
		return fmt.Errorf("list interrupted Sides: %w", err)
	}
	for _, side := range items {
		if err := s.sides.CompleteSideTurn(ctx, side.ID, side.ProcessRunID, domain.SideStatusFailed, "Side 运行因服务重启而中断", s.now()); err != nil {
			return fmt.Errorf("recover interrupted Side %s: %w", side.ID, err)
		}
	}
	return nil
}

func (s *Service) stopSideAfterStartFailure(ctx context.Context, processRunID processdomain.RunID, cause error) error {
	if stopErr := s.stopEphemeralSide(ctx, processRunID); stopErr != nil {
		return errors.Join(cause, stopErr)
	}
	return cause
}

func (s *Service) stopEphemeralSide(ctx context.Context, processRunID processdomain.RunID) error {
	ephemeral, ok := s.codex.(processdomain.CodexEphemeralThread)
	if !ok {
		return errors.New("Codex runtime does not support temporary Side questions")
	}
	if err := ephemeral.StopEphemeral(ctx, processRunID); err != nil && !errors.Is(err, processdomain.ErrProcessNotFound) {
		return fmt.Errorf("stop temporary Side question: %w", err)
	}
	return nil
}

func (s *Service) sideDTO(ctx context.Context, side domain.Side) (SideRunDTO, error) {
	stored, err := s.sides.ListSideEvents(ctx, side.ID)
	if err != nil {
		return SideRunDTO{}, err
	}
	events, err := decodeSideEvents(stored)
	if err != nil {
		return SideRunDTO{}, err
	}
	return sideRunDTO(side, events), nil
}

func encodeSideEvent(side domain.Side, event processdomain.CodexEvent) (domain.SideEvent, bool, error) {
	kind, ok := sideContentKind(event.Content)
	if !ok || event.Type == processdomain.CodexEventPlan || event.Type == processdomain.CodexEventProcessExit {
		return domain.SideEvent{}, false, nil
	}
	encoded, err := json.Marshal(event.Content)
	if err != nil {
		return domain.SideEvent{}, false, fmt.Errorf("encode Side event content: %w", err)
	}
	content := map[string]any{}
	if err := json.Unmarshal(encoded, &content); err != nil {
		return domain.SideEvent{}, false, fmt.Errorf("normalize Side event content: %w", err)
	}
	return domain.SideEvent{
		ID:     fmt.Sprintf("%s:%020d", side.ProcessRunID, event.Sequence),
		SideID: side.ID, SessionID: side.SessionID, ProcessRunID: side.ProcessRunID,
		EventID: event.EventID, Type: string(event.Type), CorrelationID: event.CorrelationID,
		TurnID: event.TurnID, Phase: string(event.Phase), ContentKind: kind, Content: content,
		TurnIndex: side.TurnIndex, Sequence: event.Sequence, CreatedAt: event.CreatedAt,
	}, true, nil
}

func sideContentKind(content processdomain.CodexEventContent) (string, bool) {
	switch content.(type) {
	case processdomain.CodexMessageContent:
		return "message", true
	case processdomain.CodexReasoningContent:
		return "reasoning", true
	case processdomain.CodexCommandContent:
		return "command", true
	case processdomain.CodexToolContent:
		return "tool", true
	case processdomain.CodexFileChangeContent:
		return "file_change", true
	case processdomain.CodexStatusContent:
		return "status", true
	case processdomain.CodexUsageContent:
		return "usage", true
	case processdomain.CodexUnknownContent:
		return "unknown", true
	default:
		return "", false
	}
}

func decodeSideEvents(stored []domain.SideEvent) ([]processdomain.CodexEvent, error) {
	result := make([]processdomain.CodexEvent, 0, len(stored))
	for _, item := range stored {
		content, err := decodeSideContent(item.ContentKind, item.Content)
		if err != nil {
			return nil, fmt.Errorf("decode Side event %s: %w", item.ID, err)
		}
		result = append(result, processdomain.CodexEvent{
			EventID: item.EventID, Type: processdomain.CodexEventType(item.Type), SessionID: processdomain.SessionID(item.SessionID),
			ProcessRunID: processdomain.RunID(item.ProcessRunID), CodexSessionID: item.SideID,
			CorrelationID: item.CorrelationID, TurnID: item.TurnID, Phase: processdomain.CodexPhase(item.Phase),
			Content: content, Sequence: item.Sequence, CreatedAt: item.CreatedAt,
		})
	}
	return result, nil
}

func decodeSideContent(kind string, value map[string]any) (processdomain.CodexEventContent, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var target any
	switch kind {
	case "message":
		target = &processdomain.CodexMessageContent{}
	case "reasoning":
		target = &processdomain.CodexReasoningContent{}
	case "command":
		target = &processdomain.CodexCommandContent{}
	case "tool":
		target = &processdomain.CodexToolContent{}
	case "file_change":
		target = &processdomain.CodexFileChangeContent{}
	case "status":
		target = &processdomain.CodexStatusContent{}
	case "usage":
		target = &processdomain.CodexUsageContent{}
	case "unknown":
		target = &processdomain.CodexUnknownContent{}
	default:
		return nil, fmt.Errorf("unsupported Side content kind %q", kind)
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		return nil, err
	}
	switch value := target.(type) {
	case *processdomain.CodexMessageContent:
		return *value, nil
	case *processdomain.CodexReasoningContent:
		return *value, nil
	case *processdomain.CodexCommandContent:
		return *value, nil
	case *processdomain.CodexToolContent:
		return *value, nil
	case *processdomain.CodexFileChangeContent:
		return *value, nil
	case *processdomain.CodexStatusContent:
		return *value, nil
	case *processdomain.CodexUsageContent:
		return *value, nil
	case *processdomain.CodexUnknownContent:
		return *value, nil
	default:
		return nil, errors.New("unsupported decoded Side content")
	}
}

func forwardSideEvents(ctx context.Context, out chan<- processdomain.CodexEvent, backlog []processdomain.CodexEvent, live <-chan processdomain.CodexEvent, unsubscribe func()) {
	defer close(out)
	defer unsubscribe()
	seen := make(map[string]struct{}, len(backlog))
	forward := func(event processdomain.CodexEvent) bool {
		key := fmt.Sprintf("%s:%d", event.ProcessRunID, event.Sequence)
		if _, ok := seen[key]; ok {
			return true
		}
		seen[key] = struct{}{}
		select {
		case <-ctx.Done():
			return false
		case out <- event:
			return true
		}
	}
	for _, event := range backlog {
		if !forward(event) {
			return
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-live:
			if !ok || !forward(event) {
				return
			}
		}
	}
}

func (s *Service) quiesceSessionSides(ctx context.Context, sessionID domain.ID) error {
	if s == nil || s.sides == nil || s.codex == nil {
		return nil
	}
	items, err := s.sides.ListSides(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("list Sides before session close: %w", err)
	}
	for _, side := range items {
		if side.Status != domain.SideStatusRunning {
			continue
		}
		runID := processdomain.RunID(side.ProcessRunID)
		if err := s.codex.Stop(ctx, runID); err != nil && !errors.Is(err, processdomain.ErrProcessNotFound) {
			return fmt.Errorf("stop Side before session close: %w", err)
		}
		if err := s.waitForSideRun(ctx, runID); err != nil {
			return fmt.Errorf("wait for Side before session close: %w", err)
		}
		_ = s.sides.CompleteSideTurn(ctx, side.ID, side.ProcessRunID, domain.SideStatusFailed, "卡片关闭时中断 Side 运行", s.now())
	}
	return nil
}

func (s *Service) cleanupSessionSides(ctx context.Context, sessionID domain.ID) error {
	if s == nil || s.sides == nil || s.codex == nil {
		return nil
	}
	items, err := s.sides.ListSides(ctx, sessionID)
	if err != nil {
		return fmt.Errorf("list Sides for session cleanup: %w", err)
	}
	for _, side := range items {
		runID := processdomain.RunID(side.ProcessRunID)
		if err := s.stopEphemeralSide(ctx, runID); err != nil {
			return fmt.Errorf("cleanup Side %s: %w", side.ID, err)
		}
		if err := s.waitForSideRun(ctx, runID); err != nil {
			return fmt.Errorf("wait for Side cleanup %s: %w", side.ID, err)
		}
	}
	if err := s.sides.DeleteSidesBySession(ctx, sessionID); err != nil {
		return fmt.Errorf("delete Sides for closed session: %w", err)
	}
	return nil
}
