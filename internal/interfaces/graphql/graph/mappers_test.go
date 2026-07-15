package graph

import (
	"reflect"
	"testing"
	"time"

	processdomain "github.com/nzlov/anycode/internal/domain/process"
	sessiondomain "github.com/nzlov/anycode/internal/domain/session"
	"github.com/nzlov/anycode/internal/interfaces/graphql/graph/model"
)

func TestMapTranscriptCommandContentPreservesInvocations(t *testing.T) {
	mapped, ok := mapTranscriptContent(processdomain.CodexCommandContent{
		Commands: []processdomain.CodexCommandInvocation{
			{Command: "npm test", Workdir: "/workspace/web"},
			{Command: "go test ./..."},
		},
		Output: "passed",
	}).(*model.TranscriptCommandContent)
	if !ok {
		t.Fatalf("mapped content = %#v", mapped)
	}
	want := []*model.TranscriptCommandInvocation{
		{Command: "npm test", Workdir: "/workspace/web"},
		{Command: "go test ./..."},
	}
	if !reflect.DeepEqual(mapped.Commands, want) || mapped.Output != "passed" {
		t.Fatalf("mapped content = %#v, want commands %#v", mapped, want)
	}
}

func TestMapSessionFileReturnsAuthenticatedReferencesWithoutDiskPath(t *testing.T) {
	createdAt := time.Now().UTC()
	mapped := mapSessionFile(sessiondomain.SessionFile{
		ID: "artifact-1", SessionID: "session-1", Role: sessiondomain.FileRoleArtifact,
		SourceType: sessiondomain.AttachmentSourceCodex, SourceID: "event-1", ArtifactKind: sessiondomain.ArtifactKindImage,
		LogicalPath: "images/result.png", Filename: "result.png", Path: "/private/archive/result.png",
		MimeType: "image/png", Size: 12, SHA256: "hash", PreviewKind: sessiondomain.PreviewKindImage, CreatedAt: createdAt,
	})
	if mapped.ID != "artifact-1" || mapped.PreviewURL == nil || *mapped.PreviewURL != "/files/artifact-1/preview" || mapped.DownloadURL != "/files/artifact-1/download" {
		t.Fatalf("mapped session file = %#v", mapped)
	}
	if mapped.LogicalPath != "images/result.png" || mapped.CreatedAt != createdAt {
		t.Fatalf("mapped session metadata = %#v", mapped)
	}
}
