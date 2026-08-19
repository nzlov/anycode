package entstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	projectdomain "github.com/nzlov/anycode/internal/domain/project"
	statisticsdomain "github.com/nzlov/anycode/internal/domain/statistics"
)

func TestStatisticsRepositoryRecordsAndQueriesMaterializedDailyStatistics(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	dayOne := time.Date(2026, 8, 18, 10, 0, 0, 0, time.Local)
	dayTwo := dayOne.Add(24 * time.Hour)
	if err := store.Projects().Save(ctx, projectdomain.Project{
		ID: "project-1", Name: "AnyCode", Path: projectdomain.ProjectPath{Value: "/repo"}, CreatedAt: dayOne, UpdatedAt: dayOne,
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Projects().Save(ctx, projectdomain.Project{
		ID: "project-2", Name: "Console", Path: projectdomain.ProjectPath{Value: "/console"}, CreatedAt: dayOne, UpdatedAt: dayOne,
	}); err != nil {
		t.Fatal(err)
	}

	filesTwo := 2
	filesFour := 4
	updates := []statisticsdomain.DailyUpdate{
		{SessionID: "session-1", ProjectID: "project-1", OccurredAt: dayOne, Created: true, TokenDelta: 20, FilesChanged: &filesTwo},
		{SessionID: "session-1", ProjectID: "project-1", OccurredAt: dayOne.Add(time.Hour), Created: true, TokenDelta: 10, FilesChanged: &filesFour},
		{SessionID: "session-1", ProjectID: "project-1", OccurredAt: dayTwo, Closed: true},
		{SessionID: "session-1", ProjectID: "project-1", OccurredAt: dayTwo.Add(time.Minute), Closed: true},
		{SessionID: "session-2", ProjectID: "project-2", OccurredAt: dayOne.Add(2 * time.Hour), Created: true, TokenDelta: 5, FilesChanged: &filesTwo},
	}
	for _, update := range updates {
		if err := store.Statistics().RecordDaily(ctx, update); err != nil {
			t.Fatal(err)
		}
	}

	dashboard, err := store.Statistics().Dashboard(ctx, "2026-08-19", statisticsdomain.DateRange{StartDay: "2026-08-18", EndDay: "2026-08-18"})
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.Today != (statisticsdomain.Metrics{ClosedCards: 1}) {
		t.Fatalf("today = %#v", dashboard.Today)
	}
	if dashboard.Total != (statisticsdomain.Metrics{CreatedCards: 2, ClosedCards: 1, FilesChanged: 6, TotalTokens: 35}) {
		t.Fatalf("total = %#v", dashboard.Total)
	}
	if len(dashboard.ByDay) != 1 || dashboard.ByDay[0].Key != "2026-08-18" || len(dashboard.ByDay[0].Projects) != 2 {
		t.Fatalf("days = %#v", dashboard.ByDay)
	}
	if dashboard.ByDay[0].Projects[0].Label != "AnyCode" || dashboard.ByDay[0].Projects[0].Metrics.TotalTokens != 30 {
		t.Fatalf("first day projects = %#v", dashboard.ByDay[0].Projects)
	}
	if dashboard.ByDay[0].Projects[1].Label != "Console" || dashboard.ByDay[0].Projects[1].Metrics.CreatedCards != 1 {
		t.Fatalf("first day projects = %#v", dashboard.ByDay[0].Projects)
	}
}

func TestStatisticsRepositoryStartsEmptyWithoutBackfill(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, OpenOptions{DatabaseURL: filepath.Join(t.TempDir(), "anycode.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	dashboard, err := store.Statistics().Dashboard(ctx, "2026-08-19", statisticsdomain.DateRange{StartDay: "2026-08-13", EndDay: "2026-08-19"})
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.Total != (statisticsdomain.Metrics{}) || len(dashboard.ByDay) != 0 {
		t.Fatalf("dashboard = %#v", dashboard)
	}
}
