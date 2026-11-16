package agentprofile

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

type SchedulePreview struct {
	Valid       bool
	Description string
	Error       string
	Timezone    string
	NextRunAt   *time.Time
}

func parseSchedule(expression string) (cron.Schedule, error) {
	fields := strings.Fields(expression)
	if len(fields) != 5 {
		return nil, fmt.Errorf("请输入五段 cron：分钟 小时 日 月 星期")
	}
	// Do not accept CRON_TZ/TZ or descriptors: all schedules use the server timezone.
	return cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow).Parse(expression)
}

func previewSchedule(expression string, now time.Time) SchedulePreview {
	preview := SchedulePreview{Valid: true, Timezone: now.In(time.Local).Format("MST (UTC-07:00)")}
	if strings.TrimSpace(expression) == "" {
		preview.Description = "未启用定时分析"
		return preview
	}
	schedule, err := parseSchedule(expression)
	if err != nil {
		preview.Valid = false
		preview.Error = "cron 表达式无效：" + err.Error()
		return preview
	}
	next := schedule.Next(now.In(time.Local))
	if next.IsZero() {
		preview.Valid = false
		preview.Error = "cron 在未来五年内没有可执行时间，请检查日期组合"
		return preview
	}
	preview.NextRunAt = &next
	fields := strings.Fields(strings.ToUpper(expression))
	labels := []string{"分钟", "小时", "日", "月", "星期"}
	parts := make([]string, 5)
	for i, field := range fields {
		parts[i] = labels[i] + "：" + describeField(field)
	}
	preview.Description = strings.Join(parts, "；")
	if !wildcardDay(fields[2]) && !wildcardDay(fields[4]) {
		preview.Description += "。日与星期任一匹配即可，其余字段须同时匹配"
	} else {
		preview.Description += "。所有指定条件同时匹配时执行"
	}
	return preview
}

func wildcardDay(field string) bool {
	for _, part := range strings.Split(field, ",") {
		base, step, stepped := strings.Cut(part, "/")
		if base != "*" && base != "?" {
			continue
		}
		if !stepped {
			return true
		}
		n, _ := strconv.Atoi(step)
		if n == 1 {
			return true
		}
	}
	return false
}

func describeField(field string) string {
	names := strings.NewReplacer("JAN", "1", "FEB", "2", "MAR", "3", "APR", "4", "MAY", "5", "JUN", "6", "JUL", "7", "AUG", "8", "SEP", "9", "OCT", "10", "NOV", "11", "DEC", "12", "SUN", "周日", "MON", "周一", "TUE", "周二", "WED", "周三", "THU", "周四", "FRI", "周五", "SAT", "周六")
	parts := strings.Split(names.Replace(field), ",")
	for i, part := range parts {
		base, step, stepped := strings.Cut(part, "/")
		if base == "*" || base == "?" {
			base = "每个值"
		} else {
			base = strings.ReplaceAll(base, "-", " 至 ")
		}
		if stepped {
			base += "，步长 " + step
		}
		parts[i] = base
	}
	return strings.Join(parts, "、")
}
