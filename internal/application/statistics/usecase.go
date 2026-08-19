package statistics

import (
	"context"
	"errors"
	"fmt"
	"time"

	statisticsdomain "github.com/nzlov/anycode/internal/domain/statistics"
)

type UseCase interface {
	Dashboard(ctx context.Context, query QueryDTO) (DashboardDTO, error)
}

type MetricsDTO = statisticsdomain.Metrics
type ProjectMetricsDTO = statisticsdomain.ProjectMetrics
type TimelineBucketDTO = statisticsdomain.TimelineBucket
type DashboardDTO = statisticsdomain.Dashboard

type QueryDTO struct {
	StartDate string
	EndDate   string
}

type Service struct {
	repository statisticsdomain.Repository
	now        func() time.Time
}

func New(repository statisticsdomain.Repository) *Service {
	return &Service{repository: repository, now: time.Now}
}

func (s *Service) Dashboard(ctx context.Context, query QueryDTO) (DashboardDTO, error) {
	if s == nil || s.repository == nil {
		return DashboardDTO{}, errors.New("statistics usecase: repository is required")
	}
	start, err := time.ParseInLocation("2006-01-02", query.StartDate, time.Local)
	if err != nil || start.Format("2006-01-02") != query.StartDate {
		return DashboardDTO{}, errors.New("statistics usecase: invalid start date")
	}
	end, err := time.ParseInLocation("2006-01-02", query.EndDate, time.Local)
	if err != nil || end.Format("2006-01-02") != query.EndDate {
		return DashboardDTO{}, errors.New("statistics usecase: invalid end date")
	}
	if end.Before(start) {
		return DashboardDTO{}, errors.New("statistics usecase: end date must not precede start date")
	}
	dashboard, err := s.repository.Dashboard(ctx, s.now().In(time.Local).Format("2006-01-02"), statisticsdomain.DateRange{
		StartDay: query.StartDate,
		EndDay:   query.EndDate,
	})
	if err != nil {
		return DashboardDTO{}, fmt.Errorf("read statistics dashboard: %w", err)
	}
	return dashboard, nil
}
