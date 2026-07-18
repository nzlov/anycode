package graph

import (
	"reflect"
	"testing"
	"time"

	sessionapp "github.com/nzlov/anycode/internal/application/session"
	processdomain "github.com/nzlov/anycode/internal/domain/process"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph/model"
)

func TestMapTranscriptCommandContentPreservesInvocations(t *testing.T) {
	mapped, ok := mapTranscriptContent(processdomain.CodexCommandContent{
		Kind: processdomain.CodexCommandExec,
		Commands: []processdomain.CodexCommandInvocation{
			{Command: "npm test", Workdir: "/workspace/web", HasOutput: true, Output: "web passed"},
			{Command: "go test ./..."},
		},
	}).(*model.TranscriptCommandContent)
	if !ok {
		t.Fatalf("mapped content = %#v", mapped)
	}
	want := []*model.TranscriptCommandInvocation{
		{Command: "npm test", Workdir: "/workspace/web", HasOutput: true, Output: "web passed"},
		{Command: "go test ./..."},
	}
	if mapped.Kind != "exec" || !reflect.DeepEqual(mapped.Commands, want) {
		t.Fatalf("mapped content = %#v, want commands %#v", mapped, want)
	}
}

func TestMapSessionDetailPreservesTodoList(t *testing.T) {
	mapped := mapSessionDetail(sessionapp.DetailDTO{
		TodoList: sessiondomain.TodoList{Items: []sessiondomain.TodoItem{
			{Text: "inspect", Completed: true},
			{Text: "verify"},
		}},
	})
	want := &model.TodoList{
		Completed: 1,
		Total:     2,
		Items: []*model.TodoItem{
			{Text: "inspect", Completed: true},
			{Text: "verify"},
		},
	}
	if !reflect.DeepEqual(mapped.TodoList, want) {
		t.Fatalf("mapped todo list = %#v, want %#v", mapped.TodoList, want)
	}
}

func TestMapSessionFileReturnsAuthenticatedReferencesWithoutDiskPath(t *testing.T) {
	createdAt := time.Now().UTC()
	mapped := mapSessionFile(sessiondomain.SessionFile{
		ID: "artifact-1", SessionID: "session-1", Role: sessiondomain.FileRoleArtifact,
		SourceType: sessiondomain.AttachmentSourceCodex, ArtifactKind: sessiondomain.ArtifactKindImage,
		LogicalPath: "images/result.png", Filename: "result.png", Path: "/private/archive/result.png",
		MimeType: "image/png", Size: 12, PreviewKind: sessiondomain.PreviewKindImage, CreatedAt: createdAt,
	})
	if mapped.ID != "artifact-1" || mapped.PreviewURL == nil || *mapped.PreviewURL != "/files/artifact-1/preview" || mapped.DownloadURL != "/files/artifact-1/download" {
		t.Fatalf("mapped session file = %#v", mapped)
	}
	if mapped.LogicalPath != "images/result.png" || mapped.CreatedAt != createdAt {
		t.Fatalf("mapped session metadata = %#v", mapped)
	}
}
