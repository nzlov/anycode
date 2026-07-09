package graph

import (
	"context"

	"github.com/nzlov/anycode/internal/application/apperror"
	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
	authapp "github.com/nzlov/anycode/internal/application/auth"
	diffapp "github.com/nzlov/anycode/internal/application/diff"
	eventapp "github.com/nzlov/anycode/internal/application/event"
	projectapp "github.com/nzlov/anycode/internal/application/project"
	questionapp "github.com/nzlov/anycode/internal/application/question"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	timelineapp "github.com/nzlov/anycode/internal/application/timeline"
	workflowapp "github.com/nzlov/anycode/internal/application/workflow"
	authdomain "github.com/nzlov/anycode/internal/domain/auth"
)

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you require here.

type UseCases struct {
	Auth        authapp.UseCase
	Projects    projectapp.UseCase
	Sessions    sessionapp.UseCase
	Events      eventapp.UseCase
	Timeline    timelineapp.UseCase
	Attachments attachmentapp.UseCase
	Diff        diffapp.UseCase
	Workflows   workflowapp.UseCase
	Questions   questionapp.UseCase
}

type Resolver struct {
	UseCases UseCases
}

func NewResolver(useCases UseCases) *Resolver {
	return &Resolver{UseCases: useCases}
}

type principalContextKey struct{}

func WithPrincipal(ctx context.Context, principal authdomain.AccessPrincipal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (authdomain.AccessPrincipal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(authdomain.AccessPrincipal)
	return principal, ok && !principal.IsZero()
}

func missingUseCase(name string) error {
	return apperror.New(apperror.CodeInternal, apperror.CategoryInfraError, "graphql usecase not configured").
		WithDetails(map[string]any{"usecase": name}).
		WithRetryable(true)
}

func unsupportedOperation(name string) error {
	return apperror.New(apperror.CodeValidationFailed, apperror.CategoryValidationError, "graphql operation is not supported").
		WithDetails(map[string]any{"operation": name})
}
