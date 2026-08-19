package entstore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	statisticsdomain "github.com/nzlov/anycode/internal/domain/statistics"
	"github.com/nzlov/anycode/internal/infra/entstore/ent"
	"github.com/nzlov/anycode/internal/infra/entstore/ent/dailystatistic"
)

type StatisticsRepository struct {
	client *ent.Client
}

func NewStatisticsRepository(client *ent.Client) *StatisticsRepository {
	return &StatisticsRepository{client: client}
}

func (r *StatisticsRepository) RecordDaily(ctx context.Context, update statisticsdomain.DailyUpdate) error {
	if r == nil || r.client == nil {
		return errors.New("statistics repository: nil client")
	}
	if strings.TrimSpace(update.SessionID) == "" || strings.TrimSpace(update.ProjectID) == "" {
		return errors.New("statistics repository: session id and project id are required")
	}
	if update.OccurredAt.IsZero() {
		return errors.New("statistics repository: occurrence time is required")
	}

	localTime := update.OccurredAt.In(time.Local)
	day := localTime.Format("2006-01-02")
	id := update.SessionID + "@" + day
	existing, err := r.client.DailyStatistic.Get(ctx, id)
	if err == nil {
		builder := r.client.DailyStatistic.UpdateOne(existing)
		if update.Created {
			builder.SetCreatedCards(1)
		}
		if update.Closed {
			builder.SetClosedCards(1)
		}
		if update.TokenDelta > 0 {
			builder.AddTotalTokens(update.TokenDelta)
		}
		if update.FilesChanged != nil {
			builder.SetFilesChanged(nonNegativeStatistic(*update.FilesChanged))
		}
		if _, err := builder.Save(ctx); err != nil {
			return fmt.Errorf("update daily statistics: %w", err)
		}
		return nil
	}
	if !ent.IsNotFound(err) {
		return fmt.Errorf("find daily statistics: %w", err)
	}

	project, err := r.client.Project.Get(ctx, update.ProjectID)
	if err != nil {
		return fmt.Errorf("find statistics project: %w", err)
	}
	create := r.client.DailyStatistic.Create().
		SetID(id).
		SetSessionID(update.SessionID).
		SetProjectID(update.ProjectID).
		SetProjectName(project.Name).
		SetDay(day).
		SetMonth(localTime.Format("2006-01")).
		SetCreatedAt(update.OccurredAt).
		SetUpdatedAt(update.OccurredAt)
	if update.Created {
		create.SetCreatedCards(1)
	}
	if update.Closed {
		create.SetClosedCards(1)
	}
	if update.TokenDelta > 0 {
		create.SetTotalTokens(update.TokenDelta)
	}
	if update.FilesChanged != nil {
		create.SetFilesChanged(nonNegativeStatistic(*update.FilesChanged))
	}
	if _, err := create.Save(ctx); err != nil {
		return fmt.Errorf("create daily statistics: %w", err)
	}
	return nil
}

func (r *StatisticsRepository) Dashboard(ctx context.Context, today string, dateRange statisticsdomain.DateRange) (statisticsdomain.Dashboard, error) {
	if r == nil || r.client == nil {
		return statisticsdomain.Dashboard{}, errors.New("statistics repository: nil client")
	}
	if strings.TrimSpace(today) == "" {
		return statisticsdomain.Dashboard{}, errors.New("statistics repository: today is required")
	}
	if strings.TrimSpace(dateRange.StartDay) == "" || strings.TrimSpace(dateRange.EndDay) == "" {
		return statisticsdomain.Dashboard{}, errors.New("statistics repository: date range is required")
	}

	todayMetrics, err := r.aggregate(ctx, r.client.DailyStatistic.Query().Where(dailystatistic.DayEQ(today)))
	if err != nil {
		return statisticsdomain.Dashboard{}, fmt.Errorf("aggregate today's statistics: %w", err)
	}
	total, err := r.aggregate(ctx, r.client.DailyStatistic.Query())
	if err != nil {
		return statisticsdomain.Dashboard{}, fmt.Errorf("aggregate total statistics: %w", err)
	}
	byDay, err := r.timeline(ctx, dateRange)
	if err != nil {
		return statisticsdomain.Dashboard{}, fmt.Errorf("aggregate daily statistics: %w", err)
	}
	return statisticsdomain.Dashboard{
		Today: todayMetrics, Total: total, ByDay: byDay,
	}, nil
}

type statisticsAggregateRow struct {
	CreatedCards *int   `json:"created_cards"`
	ClosedCards  *int   `json:"closed_cards"`
	FilesChanged *int   `json:"files_changed"`
	TotalTokens  *int64 `json:"total_tokens"`
}

func (r *StatisticsRepository) aggregate(ctx context.Context, query *ent.DailyStatisticQuery) (statisticsdomain.Metrics, error) {
	var rows []statisticsAggregateRow
	if err := query.Aggregate(statisticsAggregates()...).Scan(ctx, &rows); err != nil {
		return statisticsdomain.Metrics{}, err
	}
	if len(rows) == 0 {
		return statisticsdomain.Metrics{}, nil
	}
	return statisticsMetrics(rows[0]), nil
}

type statisticsGroupRow struct {
	Day          string `json:"day"`
	Month        string `json:"month"`
	ProjectID    string `json:"project_id"`
	ProjectName  string `json:"project_name"`
	CreatedCards *int   `json:"created_cards"`
	ClosedCards  *int   `json:"closed_cards"`
	FilesChanged *int   `json:"files_changed"`
	TotalTokens  *int64 `json:"total_tokens"`
}

func (r *StatisticsRepository) timeline(ctx context.Context, dateRange statisticsdomain.DateRange) ([]statisticsdomain.TimelineBucket, error) {
	var rows []statisticsGroupRow
	if err := r.client.DailyStatistic.Query().
		Where(dailystatistic.DayGTE(dateRange.StartDay), dailystatistic.DayLTE(dateRange.EndDay)).
		GroupBy(dailystatistic.FieldDay, dailystatistic.FieldProjectID, dailystatistic.FieldProjectName).
		Aggregate(statisticsAggregates()...).
		Scan(ctx, &rows); err != nil {
		return nil, err
	}

	type timelineProject struct {
		label   string
		metrics statisticsdomain.Metrics
	}
	grouped := make(map[string]map[string]timelineProject)
	for _, row := range rows {
		key := row.Day
		projects := grouped[key]
		if projects == nil {
			projects = make(map[string]timelineProject)
			grouped[key] = projects
		}
		project := projects[row.ProjectID]
		project.label = row.ProjectName
		project.metrics = addStatisticsMetrics(project.metrics, statisticsMetrics(statisticsAggregateRow{
			CreatedCards: row.CreatedCards, ClosedCards: row.ClosedCards, FilesChanged: row.FilesChanged, TotalTokens: row.TotalTokens,
		}))
		projects[row.ProjectID] = project
	}

	result := make([]statisticsdomain.TimelineBucket, 0, len(grouped))
	for key, projects := range grouped {
		label := key
		if len(key) == len("2006-01-02") {
			label = key[5:]
		}
		bucket := statisticsdomain.TimelineBucket{Key: key, Label: label, Projects: make([]statisticsdomain.ProjectMetrics, 0, len(projects))}
		for projectID, project := range projects {
			bucket.Projects = append(bucket.Projects, statisticsdomain.ProjectMetrics{
				Key: projectID, Label: project.label, Metrics: project.metrics,
			})
		}
		sort.Slice(bucket.Projects, func(i, j int) bool {
			if bucket.Projects[i].Label != bucket.Projects[j].Label {
				return bucket.Projects[i].Label < bucket.Projects[j].Label
			}
			return bucket.Projects[i].Key < bucket.Projects[j].Key
		})
		result = append(result, bucket)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Key < result[j].Key
	})
	return result, nil
}

func addStatisticsMetrics(left, right statisticsdomain.Metrics) statisticsdomain.Metrics {
	return statisticsdomain.Metrics{
		CreatedCards: left.CreatedCards + right.CreatedCards,
		ClosedCards:  left.ClosedCards + right.ClosedCards,
		FilesChanged: left.FilesChanged + right.FilesChanged,
		TotalTokens:  left.TotalTokens + right.TotalTokens,
	}
}

func statisticsAggregates() []ent.AggregateFunc {
	return []ent.AggregateFunc{
		ent.As(ent.Sum(dailystatistic.FieldCreatedCards), "created_cards"),
		ent.As(ent.Sum(dailystatistic.FieldClosedCards), "closed_cards"),
		ent.As(ent.Sum(dailystatistic.FieldFilesChanged), "files_changed"),
		ent.As(ent.Sum(dailystatistic.FieldTotalTokens), "total_tokens"),
	}
}

func statisticsMetrics(row statisticsAggregateRow) statisticsdomain.Metrics {
	var result statisticsdomain.Metrics
	if row.CreatedCards != nil {
		result.CreatedCards = *row.CreatedCards
	}
	if row.ClosedCards != nil {
		result.ClosedCards = *row.ClosedCards
	}
	if row.FilesChanged != nil {
		result.FilesChanged = *row.FilesChanged
	}
	if row.TotalTokens != nil {
		result.TotalTokens = *row.TotalTokens
	}
	return result
}

func nonNegativeStatistic(value int) int {
	if value < 0 {
		return 0
	}
	return value
}
