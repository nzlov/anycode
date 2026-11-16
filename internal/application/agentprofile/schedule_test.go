package agentprofile

import (
	"strings"
	"testing"
	"time"
)

func TestCronValidationAndServerTimezone(t *testing.T) {
	now := time.Date(2026, 11, 16, 2, 50, 0, 0, time.Local)
	for _, expression := range []string{"", "0 3 * * *", "*/15 8-18 * * MON-FRI", "0 3 1,15 * *", "0 3 * JAN,MAR SUN"} {
		preview := previewSchedule(expression, now)
		if !preview.Valid || preview.Description == "" || preview.Timezone == "" {
			t.Errorf("%q: %#v", expression, preview)
		}
	}
	for _, expression := range []string{"* * * *", "0 0 3 * * *", "60 3 * * *", "0 3 31 2 *", "@daily", "CRON_TZ=UTC 0 3 * * *", "*/0 * * * *", "0 3 * * 7"} {
		if preview := previewSchedule(expression, now); preview.Valid || preview.Error == "" {
			t.Errorf("invalid %q accepted: %#v", expression, preview)
		}
	}
	p := previewSchedule("0 3 * * *", now)
	if p.NextRunAt == nil || !p.NextRunAt.Equal(time.Date(2026, 11, 16, 3, 0, 0, 0, time.Local)) {
		t.Fatalf("next = %#v", p.NextRunAt)
	}
	if p.NextRunAt.Location() != time.Local {
		t.Fatal("schedule changed the server timezone")
	}
	p = previewSchedule("0 3 1 * MON", now)
	if !strings.Contains(p.Description, "任一匹配") {
		t.Fatal(p.Description)
	}
	if p := previewSchedule("0 3 */1 * MON", now); strings.Contains(p.Description, "任一匹配") {
		t.Fatal(p.Description)
	}
}
