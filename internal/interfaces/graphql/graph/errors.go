package graph

import (
	"context"

	"github.com/99designs/gqlgen/graphql"
	"github.com/nzlov/anycode/internal/application/apperror"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

func ErrorPresenter(ctx context.Context, err error) *gqlerror.Error {
	gqlErr := graphql.DefaultErrorPresenter(ctx, err)
	appErr, ok := apperror.From(err)
	if !ok {
		appErr = apperror.Wrap(err, apperror.CodeInternal, apperror.CategoryInfraError, "request failed")
	}
	gqlErr.Message = appErr.PublicMessage()
	gqlErr.Extensions = map[string]any{
		"code":       appErr.Code,
		"category":   string(appErr.Category),
		"retryable":  appErr.Retryable,
		"userAction": appErr.UserAction,
	}
	if details := appErr.PublicDetails(); len(details) > 0 {
		gqlErr.Extensions["details"] = details
	}
	return gqlErr
}
