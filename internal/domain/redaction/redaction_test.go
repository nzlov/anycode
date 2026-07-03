package redaction

import "testing"

func TestMapRedactsSensitiveKeysAndAbsolutePaths(t *testing.T) {
	got := Map(map[string]any{
		"accessKey":    "secret",
		"worktreePath": "/home/nzlov/workspaces/github/project",
		"filePath":     "internal/main.go",
		"nested": map[string]any{
			"tursoAuthToken": "token",
			"repoPath":       "/workspaces/project",
		},
	})

	if got["accessKey"] != Redacted {
		t.Fatalf("accessKey = %#v", got["accessKey"])
	}
	if got["worktreePath"] != RedactedPath {
		t.Fatalf("worktreePath = %#v", got["worktreePath"])
	}
	if got["filePath"] != "internal/main.go" {
		t.Fatalf("relative filePath = %#v", got["filePath"])
	}
	nested := got["nested"].(map[string]any)
	if nested["tursoAuthToken"] != Redacted || nested["repoPath"] != RedactedPath {
		t.Fatalf("nested = %#v", nested)
	}
}

func TestTextRedactsAbsolutePathAndTokenAssignment(t *testing.T) {
	got := Text(`open /home/nzlov/workspaces/github/project: token=abc failed`)
	if got != "open [redacted_path]: token=[redacted] failed" {
		t.Fatalf("Text() = %q", got)
	}
}
