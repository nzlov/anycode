package auth

import (
	"context"

	domain "github.com/nzlov/anycode/internal/domain/auth"
)

type UseCase interface {
	AuthorizeRequest(ctx context.Context, input AuthorizeInput) (domain.AccessPrincipal, error)
}

type AuthorizeInput struct {
	PresentedKey string
	Kind         string
}
