package graph

import (
	"reflect"
	"testing"

	processdomain "github.com/nzlov/anycode/internal/domain/process"
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
