package session

import (
	"context"
	"sync"

	"github.com/nzlov/anycode/internal/application/port"
	domain "github.com/nzlov/anycode/internal/domain/session"
)

var _ port.SessionLocker = (*MemorySessionLocker)(nil)

type MemorySessionLocker struct {
	mu    sync.Mutex
	locks map[domain.ID]*sync.Mutex
}

func NewMemorySessionLocker() *MemorySessionLocker {
	return &MemorySessionLocker{locks: map[domain.ID]*sync.Mutex{}}
}

func (l *MemorySessionLocker) WithSessionLock(ctx context.Context, id domain.ID, fn func(context.Context) error) error {
	lock := l.lockFor(id)
	lock.Lock()
	defer lock.Unlock()
	return fn(ctx)
}

func (l *MemorySessionLocker) lockFor(id domain.ID) *sync.Mutex {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.locks == nil {
		l.locks = map[domain.ID]*sync.Mutex{}
	}
	lock, ok := l.locks[id]
	if !ok {
		lock = &sync.Mutex{}
		l.locks[id] = lock
	}
	return lock
}
