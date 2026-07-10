package question

import (
	"context"
	"errors"
	"fmt"
	"sync"

	domain "github.com/nzlov/anycode/internal/domain/question"
)

var ErrWaitCancelled = errors.New("question wait cancelled")

type MemoryAnswerWaiter struct {
	mu      sync.Mutex
	entries map[domain.BatchID]*waitEntry
}

type waitEntry struct {
	done    chan struct{}
	answers []domain.Answer
	err     error
	closed  bool
}

func NewMemoryAnswerWaiter() *MemoryAnswerWaiter {
	return &MemoryAnswerWaiter{entries: map[domain.BatchID]*waitEntry{}}
}

func (w *MemoryAnswerWaiter) Prepare(_ context.Context, batchID domain.BatchID) error {
	if w == nil {
		return errors.New("question answer waiter is nil")
	}
	w.entry(batchID)
	return nil
}

func (w *MemoryAnswerWaiter) Wait(ctx context.Context, batchID domain.BatchID) ([]domain.Answer, error) {
	if w == nil {
		return nil, errors.New("question answer waiter is nil")
	}
	w.mu.Lock()
	entry := w.entries[batchID]
	w.mu.Unlock()
	if entry == nil {
		return nil, ErrWaitCancelled
	}
	select {
	case <-ctx.Done():
		w.removeIfPending(batchID, entry)
		return nil, ctx.Err()
	case <-entry.done:
		w.remove(batchID, entry)
		if entry.err != nil {
			return nil, entry.err
		}
		return append([]domain.Answer(nil), entry.answers...), nil
	}
}

func (w *MemoryAnswerWaiter) Resume(_ context.Context, batchID domain.BatchID, answers []domain.Answer) error {
	if w == nil {
		return errors.New("question answer waiter is nil")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	entry := w.entries[batchID]
	if entry == nil {
		return nil
	}
	if entry.closed {
		return nil
	}
	entry.answers = append([]domain.Answer(nil), answers...)
	entry.closed = true
	close(entry.done)
	return nil
}

func (w *MemoryAnswerWaiter) Cancel(_ context.Context, batchID domain.BatchID, reason string) error {
	if w == nil {
		return errors.New("question answer waiter is nil")
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	entry := w.entries[batchID]
	if entry == nil {
		return nil
	}
	if entry.closed {
		return nil
	}
	if reason == "" {
		entry.err = ErrWaitCancelled
	} else {
		entry.err = fmt.Errorf("%w: %s", ErrWaitCancelled, reason)
	}
	entry.closed = true
	close(entry.done)
	return nil
}

func (w *MemoryAnswerWaiter) Forget(batchID domain.BatchID) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.entries, batchID)
}

func (w *MemoryAnswerWaiter) entry(batchID domain.BatchID) *waitEntry {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.entries == nil {
		w.entries = map[domain.BatchID]*waitEntry{}
	}
	entry := w.entries[batchID]
	if entry == nil {
		entry = &waitEntry{done: make(chan struct{})}
		w.entries[batchID] = entry
	}
	return entry
}

func (w *MemoryAnswerWaiter) remove(batchID domain.BatchID, entry *waitEntry) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.entries[batchID] == entry {
		delete(w.entries, batchID)
	}
}

func (w *MemoryAnswerWaiter) removeIfPending(batchID domain.BatchID, entry *waitEntry) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.entries[batchID] == entry && !entry.closed {
		delete(w.entries, batchID)
	}
}
