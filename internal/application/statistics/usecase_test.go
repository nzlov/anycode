package statistics

import (
	"context"
	"testing"
	"time"

	statisticsdomain "github.com/nzlov/anycode/internal/domain/statistics"
)

func TestDashboardReadsMaterializedStatisticsForServerLocalDay(t *testing.T) {
	repository := &fakeRepository{dashboard: statisticsdomain.Dashboard{
		Today: statisticsdomain.Metrics{CreatedCards: 2},
		Total: statisticsdomain.Metrics{CreatedCards: 5},
	}}
	service := New(repository)
	service.now = func() time.Time { return time.Date(2026, 8, 19, 12, 0, 0, 0, time.Local) }

	got, err := service.Dashboard(context.Background(), QueryDTO{StartDate: "2026-08-13", EndDate: "2026-08-19"})
	if err != nil {
		t.Fatal(err)
	}
	if repository.today != "2026-08-19" || repository.dateRange != (statisticsdomain.DateRange{StartDay: "2026-08-13", EndDay: "2026-08-19"}) || got.Today.CreatedCards != 2 || got.Total.CreatedCards != 5 {
		t.Fatalf("today = %q, range = %#v, dashboard = %#v", repository.today, repository.dateRange, got)
	}
}

func TestDashboardRejectsInvalidDateRange(t *testing.T) {
	service := New(&fakeRepository{})
	for _, query := range []QueryDTO{
		{StartDate: "2026-02-30", EndDate: "2026-03-01"},
		{StartDate: "2026-08-20", EndDate: "2026-08-19"},
	} {
		if _, err := service.Dashboard(context.Background(), query); err == nil {
			t.Fatalf("Dashboard(%#v) succeeded", query)
		}
	}
}

type fakeRepository struct {
	today     string
	dateRange statisticsdomain.DateRange
	dashboard statisticsdomain.Dashboard
	err       error
}

func (f *fakeRepository) RecordDaily(context.Context, statisticsdomain.DailyUpdate) error {
	return nil
}

func (f *fakeRepository) Dashboard(_ context.Context, today string, dateRange statisticsdomain.DateRange) (statisticsdomain.Dashboard, error) {
	f.today = today
	f.dateRange = dateRange
	return f.dashboard, f.err
}
