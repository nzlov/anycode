package setting

import (
	"context"
	"errors"
	"time"
)

type QuickCommandID string

type QuickCommand struct {
	ID        QuickCommandID
	Content   string
	CreatedAt time.Time
}

type QuickCommandQuery struct {
	Page     int
	PageSize int
}

type QuickCommandPage struct {
	Items    []QuickCommand
	Page     int
	PageSize int
	Total    int
}

var ErrQuickCommandNotFound = errors.New("quick command not found")

type Repository interface {
	Create(ctx context.Context, command QuickCommand) error
	List(ctx context.Context, query QuickCommandQuery) (QuickCommandPage, error)
	Delete(ctx context.Context, id QuickCommandID) error
}
