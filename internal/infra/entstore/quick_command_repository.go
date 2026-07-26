package entstore

import (
	"context"
	"fmt"

	"github.com/nzlov/anycode/internal/domain/setting"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	entquickcommand "github.com/nzlov/anycode/internal/infra/entstore/ent/quickcommand"
)

type SettingRepository struct {
	client *ent.Client
}

func NewSettingRepository(client *ent.Client) *SettingRepository {
	return &SettingRepository{client: client}
}

func (r *SettingRepository) Create(ctx context.Context, command setting.QuickCommand) error {
	create := r.client.QuickCommand.Create().
		SetID(string(command.ID)).
		SetContent(command.Content)
	if command.ProjectID != nil {
		create.SetProjectID(string(*command.ProjectID))
	}
	if !command.CreatedAt.IsZero() {
		create.SetCreatedAt(command.CreatedAt)
	}
	if _, err := create.Save(ctx); err != nil {
		return fmt.Errorf("create quick command: %w", err)
	}
	return nil
}

func (r *SettingRepository) Update(ctx context.Context, id setting.QuickCommandID, content string) (setting.QuickCommand, error) {
	row, err := r.client.QuickCommand.UpdateOneID(string(id)).SetContent(content).Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return setting.QuickCommand{}, setting.ErrQuickCommandNotFound
		}
		return setting.QuickCommand{}, fmt.Errorf("update quick command: %w", err)
	}
	return setting.QuickCommand{
		ID:        setting.QuickCommandID(row.ID),
		ProjectID: quickCommandProjectID(row.ProjectID),
		Content:   row.Content,
		CreatedAt: row.CreatedAt,
	}, nil
}

func (r *SettingRepository) List(ctx context.Context, query setting.QuickCommandQuery) (setting.QuickCommandPage, error) {
	query = normalizeQuickCommandQuery(query)
	commandsQuery := r.client.QuickCommand.Query()
	if query.ProjectID == nil {
		commandsQuery.Where(entquickcommand.ProjectIDIsNil())
	} else if query.IncludeGlobal {
		commandsQuery.Where(entquickcommand.Or(
			entquickcommand.ProjectIDIsNil(),
			entquickcommand.ProjectIDEQ(string(*query.ProjectID)),
		))
	} else {
		commandsQuery.Where(entquickcommand.ProjectIDEQ(string(*query.ProjectID)))
	}
	total, err := commandsQuery.Clone().Count(ctx)
	if err != nil {
		return setting.QuickCommandPage{}, fmt.Errorf("count quick commands: %w", err)
	}
	maxPage := 1
	if total > 0 {
		maxPage = (total + query.PageSize - 1) / query.PageSize
	}
	if query.Page > maxPage {
		query.Page = maxPage
	}
	rows, err := commandsQuery.
		Order(ent.Desc(entquickcommand.FieldCreatedAt), ent.Desc(entquickcommand.FieldID)).
		Offset((query.Page - 1) * query.PageSize).
		Limit(query.PageSize).
		All(ctx)
	if err != nil {
		return setting.QuickCommandPage{}, fmt.Errorf("list quick commands: %w", err)
	}
	commands := make([]setting.QuickCommand, 0, len(rows))
	for _, row := range rows {
		commands = append(commands, setting.QuickCommand{
			ID:        setting.QuickCommandID(row.ID),
			ProjectID: quickCommandProjectID(row.ProjectID),
			Content:   row.Content,
			CreatedAt: row.CreatedAt,
		})
	}
	return setting.QuickCommandPage{
		Items:    commands,
		Page:     query.Page,
		PageSize: query.PageSize,
		Total:    total,
	}, nil
}

func quickCommandProjectID(value *string) *setting.QuickCommandProjectID {
	if value == nil {
		return nil
	}
	projectID := setting.QuickCommandProjectID(*value)
	return &projectID
}

func normalizeQuickCommandQuery(query setting.QuickCommandQuery) setting.QuickCommandQuery {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = 20
	}
	if query.PageSize > 100 {
		query.PageSize = 100
	}
	return query
}

func (r *SettingRepository) Delete(ctx context.Context, id setting.QuickCommandID) error {
	if err := r.client.QuickCommand.DeleteOneID(string(id)).Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			return setting.ErrQuickCommandNotFound
		}
		return fmt.Errorf("delete quick command: %w", err)
	}
	return nil
}
