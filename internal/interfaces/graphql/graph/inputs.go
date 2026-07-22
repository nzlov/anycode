package graph

import (
	"github.com/99designs/gqlgen/graphql"
	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph/model"
)

func buildSessionConfig(input *model.SessionConfigInput) sessionapp.ConfigInput {
	if input == nil {
		return sessionapp.ConfigInput{}
	}
	return sessionapp.ConfigInput{
		CodexModel:      stringValue(input.CodexModel, ""),
		ReasoningEffort: stringValue(input.ReasoningEffort, ""),
		PermissionMode:  stringValue(input.PermissionMode, ""),
		FastMode:        input.FastMode,
	}
}

func buildListSessionsInput(input *model.ListSessionsInput) sessionapp.ListSessionsInput {
	if input == nil {
		return sessionapp.ListSessionsInput{}
	}
	return sessionapp.ListSessionsInput{
		ProjectID:     sessionProjectIDPtr(input.ProjectID),
		Scope:         stringValue(input.Scope, ""),
		Range:         stringValue(input.Range, ""),
		OlderThanDays: intValue(input.OlderThanDays, 0),
		Page:          intValue(input.Page, 0),
		PageSize:      intValue(input.PageSize, 0),
		Filter:        stringValue(input.Filter, ""),
		Sort:          stringValue(input.Sort, ""),
	}
}

func buildCleanupSessionsInput(input model.CleanupSessionsInput) sessionapp.CleanupSessionsInput {
	return sessionapp.CleanupSessionsInput{
		ProjectID:     sessionProjectIDPtr(input.ProjectID),
		Scope:         stringValue(input.Scope, ""),
		Filter:        stringValue(input.Filter, ""),
		OlderThanDays: input.OlderThanDays,
	}
}

func sessionProjectIDPtr(value *string) *sessiondomain.ProjectID {
	if value == nil {
		return nil
	}
	id := sessiondomain.ProjectID(*value)
	return &id
}

func attachmentInput(file graphql.Upload, ownerKeyHash string) attachmentapp.StageAttachmentInput {
	return attachmentapp.StageAttachmentInput{
		OwnerKeyHash: ownerKeyHash,
		Filename:     file.Filename,
		MimeType:     file.ContentType,
		Size:         file.Size,
		Reader:       file.File,
	}
}
