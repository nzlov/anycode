package graph

import (
	"github.com/99designs/gqlgen/graphql"
	attachmentapp "github.com/nzlov/anycode/internal/application/attachment"
	sessionapp "github.com/nzlov/anycode/internal/application/session"
	eventdomain "github.com/nzlov/anycode/internal/domain/event"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph/model"
)

func buildSessionConfig(input *model.SessionConfigInput) sessiondomain.Config {
	if input == nil {
		return sessiondomain.Config{}
	}
	return sessiondomain.Config{
		CodexModel:      stringValue(input.CodexModel, ""),
		ReasoningEffort: stringValue(input.ReasoningEffort, ""),
		PermissionMode:  stringValue(input.PermissionMode, ""),
	}
}

func buildListSessionsInput(input *model.ListSessionsInput) sessionapp.ListSessionsInput {
	if input == nil {
		return sessionapp.ListSessionsInput{}
	}
	return sessionapp.ListSessionsInput{
		ProjectID: sessionProjectIDPtr(input.ProjectID),
		Scope:     stringValue(input.Scope, ""),
		Range:     stringValue(input.Range, ""),
		Page:      intValue(input.Page, 0),
		PageSize:  intValue(input.PageSize, 0),
		Filter:    stringValue(input.Filter, ""),
		Sort:      stringValue(input.Sort, ""),
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

func buildEventScope(input model.SessionEventsInput) eventdomain.Scope {
	return eventdomain.Scope{
		SessionID: eventSessionIDPtr(input.SessionID),
		ProjectID: stringValue(input.ProjectID, ""),
	}
}

func eventSessionIDPtr(value *string) *eventdomain.SessionID {
	if value == nil {
		return nil
	}
	id := eventdomain.SessionID(*value)
	return &id
}
