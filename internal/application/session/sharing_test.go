package session

import (
	"context"
	domain "github.com/nzlov/anycode/internal/domain/session"
	"testing"
)

func TestApprovedMessageAppendIsIdempotentAfterAttachmentsAreConsumed(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["s"] = domain.Session{ID: "s", ProjectID: "p", Status: domain.StatusWaitingApproval}
	repo.stagedAttachments["file"] = domain.StagedAttachment{ID: "file", Filename: "reviewed.txt", Path: "/staged/file/reviewed.txt"}
	files := newFakeAttachmentStore()
	service := New(repo, newFakeProjectRepository("p"), WithAttachments(repo, files), WithSessionLocker(NewMemorySessionLocker()))
	input := AppendPromptInput{SessionID: "s", Body: "reviewed content", StagedAttachmentIDs: []domain.StagedAttachmentID{"file"}, ApprovedMessageID: "review-1"}
	first, err := service.AppendPrompt(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.AppendPrompt(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != "share-review-1" || first.ID != second.ID || len(repo.appends) != 1 {
		t.Fatalf("duplicate append: %v %v %#v", first, second, repo.appends)
	}
}
func TestApprovedMessageCannotBypassIdleOrReviewedConfig(t *testing.T) {
	repo := newFakeRepository()
	repo.sessions["s"] = domain.Session{ID: "s", ProjectID: "p", Status: domain.StatusRunning, Config: domain.Config{CodexModel: "model"}}
	service := New(repo, newFakeProjectRepository("p"))
	config := domain.Config{CodexModel: "model"}
	input := AppendPromptInput{SessionID: "s", Body: "reviewed", ExpectedConfig: &config, RequireIdle: true, ApprovedMessageID: "a"}
	if _, err := service.AppendPrompt(context.Background(), input); err == nil {
		t.Fatal("injected message into running turn")
	}
	v := repo.sessions["s"]
	v.Status = domain.StatusStopped
	repo.sessions["s"] = v
	config.CodexModel = "changed"
	if _, err := service.AppendPrompt(context.Background(), input); err == nil {
		t.Fatal("reviewed config was ignored")
	}
	if len(repo.appends) != 0 {
		t.Fatal("invalid reviewed message persisted")
	}
}
