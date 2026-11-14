package feishu

import (
	"time"
	_ "time/tzdata" // Minimal runtime images also honor the server's TZ setting.
)

// FormatCardTime renders stored UTC timestamps in the server's local timezone.
func FormatCardTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.In(time.Local).Format("2006-01-02 15:04:05 -07:00")
}
