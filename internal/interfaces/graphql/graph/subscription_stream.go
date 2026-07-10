package graph

import (
	"context"

	"github.com/nzlov/anycode/internal/interfaces/graphql/graph/model"
)

func sendSessionStateItem(ctx context.Context, out chan<- *model.SessionStateStreamItem, item *model.SessionStateStreamItem) bool {
	select {
	case out <- item:
		return true
	case <-ctx.Done():
		return false
	}
}
