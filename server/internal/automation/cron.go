package automation

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// PresetCron 将预设转为 5 段 cron（分 时 日 月 周）。
func PresetCron(preset string) string {
	switch strings.ToLower(strings.TrimSpace(preset)) {
	case "daily":
		return "0 9 * * *"
	case "weekly":
		return "0 9 * * 1"
	case "monthly":
		return "0 9 1 * *"
	case "yearly":
		return "0 9 1 1 *"
	default:
		return ""
	}
}

// ParseCronConfig 从 TriggerConfig 解析 cron；支持 preset 自动填 expr。
func ParseCronConfig(raw []byte) (CronConfig, error) {
	var cfg CronConfig
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return cfg, err
		}
	}
	if cfg.Expr == "" && cfg.Preset != "" {
		cfg.Expr = PresetCron(cfg.Preset)
	}
	if cfg.Expr == "" {
		return cfg, fmt.Errorf("%w: cron expr 为空", ErrInvalidInput)
	}
	if _, err := parseCron(cfg.Expr); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// cronDue 判断 now 是否落入 expr 的当前分钟槽，且相对 lastFired 尚未打过该槽。
// misfire：只打「当前分钟」这一期，跳过历史错过窗口。
func cronDue(expr string, tz *time.Location, lastFired *time.Time, now time.Time) (bool, error) {
	sched, err := parseCron(expr)
	if err != nil {
		return false, err
	}
	local := now.In(tz)
	slot := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), local.Minute(), 0, 0, tz)
	if !sched.matches(slot) {
		return false, nil
	}
	if lastFired != nil {
		lf := lastFired.In(tz)
		lfSlot := time.Date(lf.Year(), lf.Month(), lf.Day(), lf.Hour(), lf.Minute(), 0, 0, tz)
		if !lfSlot.Before(slot) {
			return false, nil
		}
	}
	return true, nil
}

type cronSched struct {
	min, hour, dom, mon, dow fieldSet
}

type fieldSet struct {
	all  bool
	vals map[int]bool
}

func (f fieldSet) match(v int) bool {
	if f.all {
		return true
	}
	return f.vals[v]
}

func (s cronSched) matches(t time.Time) bool {
	// Go weekday: Sunday=0；cron 常用 0/7=Sun
	dow := int(t.Weekday())
	return s.min.match(t.Minute()) &&
		s.hour.match(t.Hour()) &&
		s.dom.match(t.Day()) &&
		s.mon.match(int(t.Month())) &&
		(s.dow.match(dow) || (dow == 0 && s.dow.match(7)))
}

func parseCron(expr string) (cronSched, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return cronSched{}, fmt.Errorf("%w: cron 需 5 段 (分 时 日 月 周)", ErrInvalidInput)
	}
	min, err := parseField(parts[0], 0, 59)
	if err != nil {
		return cronSched{}, err
	}
	hour, err := parseField(parts[1], 0, 23)
	if err != nil {
		return cronSched{}, err
	}
	dom, err := parseField(parts[2], 1, 31)
	if err != nil {
		return cronSched{}, err
	}
	mon, err := parseField(parts[3], 1, 12)
	if err != nil {
		return cronSched{}, err
	}
	dow, err := parseField(parts[4], 0, 7)
	if err != nil {
		return cronSched{}, err
	}
	return cronSched{min: min, hour: hour, dom: dom, mon: mon, dow: dow}, nil
}

func parseField(s string, min, max int) (fieldSet, error) {
	s = strings.TrimSpace(s)
	if s == "*" {
		return fieldSet{all: true}, nil
	}
	out := fieldSet{vals: map[int]bool{}}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		step := 1
		rangePart := part
		if strings.Contains(part, "/") {
			bits := strings.SplitN(part, "/", 2)
			rangePart = bits[0]
			n, err := strconv.Atoi(bits[1])
			if err != nil || n <= 0 {
				return fieldSet{}, fmt.Errorf("%w: 非法 step %q", ErrInvalidInput, part)
			}
			step = n
			if rangePart == "*" {
				rangePart = fmt.Sprintf("%d-%d", min, max)
			}
		}
		lo, hi := 0, 0
		if strings.Contains(rangePart, "-") {
			bits := strings.SplitN(rangePart, "-", 2)
			var err error
			lo, err = strconv.Atoi(bits[0])
			if err != nil {
				return fieldSet{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
			}
			hi, err = strconv.Atoi(bits[1])
			if err != nil {
				return fieldSet{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
			}
		} else {
			n, err := strconv.Atoi(rangePart)
			if err != nil {
				return fieldSet{}, fmt.Errorf("%w: %v", ErrInvalidInput, err)
			}
			lo, hi = n, n
		}
		if lo < min || hi > max || lo > hi {
			return fieldSet{}, fmt.Errorf("%w: 字段超出范围 %q", ErrInvalidInput, part)
		}
		for v := lo; v <= hi; v += step {
			out.vals[v] = true
		}
	}
	return out, nil
}

// loadLocation 加载时区，失败回退 UTC。
func loadLocation(name string) *time.Location {
	if name == "" {
		name = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}
