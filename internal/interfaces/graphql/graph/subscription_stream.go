package graph

import (
	"context"

	"github.com/nzlov/anycode/internal/interfaces/graphql/graph/model"
)

func sendSessionEventItem(ctx context.Context, out chan<- *model.SessionEventStreamItem, item *model.SessionEventStreamItem) bool {
	select {
	case out <- item:
		return true
	case <-ctx.Done():
		return false
	}
}
