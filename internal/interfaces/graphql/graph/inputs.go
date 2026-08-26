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

func promptMentionsFromInput(input []*model.PromptMentionInput) []sessiondomain.PromptMention {
	mentions := make([]sessiondomain.PromptMention, 0, len(input))
	for _, mention := range input {
		if mention != nil {
			mentions = append(mentions, sessiondomain.PromptMention{Path: mention.Path})
		}
	}
	return mentions
}

func promptFileReferencesFromInput(input []*model.PromptFileReferenceInput) []sessiondomain.PromptFileReference {
	references := make([]sessiondomain.PromptFileReference, 0, len(input))
	for _, reference := range input {
		if reference == nil {
			continue
		}
		references = append(references, sessiondomain.PromptFileReference{
			Kind:          sessiondomain.PromptFileReferenceKind(reference.Kind),
			SessionFileID: sessiondomain.SessionFileID(stringValue(reference.SessionFileID, "")),
			FilePath:      stringValue(reference.FilePath, ""),
			Version:       stringValue(reference.Version, ""),
		})
	}
	return references
}

func promptAnnotationsFromInput(input []*model.PromptAnnotationInput) []sessiondomain.PromptAnnotation {
	annotations := make([]sessiondomain.PromptAnnotation, 0, len(input))
	for _, annotation := range input {
		if annotation == nil {
			continue
		}
		marks := make([]sessiondomain.PromptAnnotationMark, 0, len(annotation.Marks))
		for _, mark := range annotation.Marks {
			if mark == nil {
				continue
			}
			marks = append(marks, sessiondomain.PromptAnnotationMark{
				ID: mark.ID, Kind: mark.Kind, Shape: stringValue(mark.Shape, ""),
				X: floatValue(mark.X, 0), Y: floatValue(mark.Y, 0),
				Width: floatValue(mark.Width, 0), Height: floatValue(mark.Height, 0),
				Start: promptAnnotationPositionFromInput(mark.Start),
				End:   promptAnnotationPositionFromInput(mark.End),
				Quote: stringValue(mark.Quote, ""), Note: stringValue(mark.Note, ""),
			})
		}
		annotations = append(annotations, sessiondomain.PromptAnnotation{
			ID: annotation.ID, Source: annotation.Source, Content: annotation.Content,
			Marks: marks, FileReferences: promptFileReferencesFromInput(annotation.FileReferences),
		})
	}
	return annotations
}

func promptAnnotationPositionFromInput(input *model.PromptAnnotationPositionInput) *sessiondomain.PromptAnnotationPosition {
	if input == nil {
		return nil
	}
	return &sessiondomain.PromptAnnotationPosition{
		Line: input.Line, Column: input.Column, Revision: stringValue(input.Revision, ""),
	}
}

func floatValue(value *float64, fallback float64) float64 {
	if value == nil {
		return fallback
	}
	return *value
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
