package setting

import (
	"context"
	"testing"
	"time"

	"github.com/nzlov/anycode/internal/application/apperror"
	domain "github.com/nzlov/anycode/internal/domain/setting"
)

func TestCreateQuickCommandAllowsDuplicateContent(t *testing.T) {
	repo := &fakeRepository{}
	service := New(repo)
	service.now = func() time.Time { return time.Unix(10, 0).UTC() }
	ids := []domain.QuickCommandID{"command-1", "command-2"}
	service.generateID = func() (domain.QuickCommandID, error) {
		id := ids[0]
		ids = ids[1:]
		return id, nil
	}

	first, err := service.CreateQuickCommand(context.Background(), CreateQuickCommandInput{Content: "检查测试"})
	if err != nil {
		t.Fatalf("CreateQuickCommand() first error = %v", err)
	}
	second, err := service.CreateQuickCommand(context.Background(), CreateQuickCommandInput{Content: "检查测试"})
	if err != nil {
		t.Fatalf("CreateQuickCommand() second error = %v", err)
	}
	if first.ID == second.ID || first.Content != second.Content || len(repo.commands) != 2 {
		t.Fatalf("duplicate commands = first:%#v second:%#v stored:%#v", first, second, repo.commands)
	}
}

func TestDeleteQuickCommandUsesID(t *testing.T) {
	repo := &fakeRepository{commands: []domain.QuickCommand{
		{ID: "command-1", Content: "检查测试"},
		{ID: "command-2", Content: "检查测试"},
	}}
	service := New(repo)

	if err := service.DeleteQuickCommand(context.Background(), DeleteQuickCommandInput{ID: "command-1"}); err != nil {
		t.Fatalf("DeleteQuickCommand() error = %v", err)
	}
	if len(repo.commands) != 1 || repo.commands[0].ID != "command-2" {
		t.Fatalf("commands after delete = %#v", repo.commands)
	}
}

func TestListQuickCommandsNormalizesPagination(t *testing.T) {
	repo := &fakeRepository{page: domain.QuickCommandPage{
		Items:    []domain.QuickCommand{{ID: "command-1", Content: "检查测试"}},
		Page:     1,
		PageSize: 100,
		Total:    3,
	}}
	service := New(repo)

	page, err := service.ListQuickCommands(context.Background(), ListQuickCommandsInput{Page: -1, PageSize: 500})
	if err != nil {
		t.Fatalf("ListQuickCommands() error = %v", err)
	}
	if repo.listQuery.Page != 1 || repo.listQuery.PageSize != 100 {
		t.Fatalf("list query = %#v", repo.listQuery)
	}
	if page.Page != 1 || page.PageSize != 100 || page.Total != 3 || len(page.Items) != 1 {
		t.Fatalf("page = %#v", page)
	}
}

func TestQuickCommandValidationErrorsAreStructured(t *testing.T) {
	service := New(&fakeRepository{})

	_, err := service.CreateQuickCommand(context.Background(), CreateQuickCommandInput{Content: "   "})
	assertAppError(t, err, apperror.CodeValidationFailed)
	err = service.DeleteQuickCommand(context.Background(), DeleteQuickCommandInput{})
	assertAppError(t, err, apperror.CodeValidationFailed)
}

func TestDeleteQuickCommandMapsNotFound(t *testing.T) {
	repo := &fakeRepository{deleteErr: domain.ErrQuickCommandNotFound}
	service := New(repo)

	err := service.DeleteQuickCommand(context.Background(), DeleteQuickCommandInput{ID: "missing"})
	assertAppError(t, err, apperror.CodeNotFound)
}

func assertAppError(t *testing.T, err error, code string) {
	t.Helper()
	appErr, ok := apperror.From(err)
	if !ok || appErr.Code != code || appErr.Category != apperror.CategoryValidationError {
		t.Fatalf("error = %#v", err)
	}
}

type fakeRepository struct {
	commands  []domain.QuickCommand
	page      domain.QuickCommandPage
	listQuery domain.QuickCommandQuery
	deleteErr error
}

func (r *fakeRepository) Create(_ context.Context, command domain.QuickCommand) error {
	r.commands = append(r.commands, command)
	return nil
}

func (r *fakeRepository) List(_ context.Context, query domain.QuickCommandQuery) (domain.QuickCommandPage, error) {
	r.listQuery = query
	if r.page.PageSize != 0 {
		return r.page, nil
	}
	return domain.QuickCommandPage{
		Items:    append([]domain.QuickCommand(nil), r.commands...),
		Page:     query.Page,
		PageSize: query.PageSize,
		Total:    len(r.commands),
	}, nil
}

func (r *fakeRepository) Delete(_ context.Context, id domain.QuickCommandID) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	for index, command := range r.commands {
		if command.ID == id {
			r.commands = append(r.commands[:index], r.commands[index+1:]...)
			return nil
		}
	}
	return nil
}
