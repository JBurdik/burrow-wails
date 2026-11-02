package control

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Recurrence is a scheduled prompt's rule (t3code's ScheduledTaskSchedule,
// without webhooks). A run is computed from the moment it fires, never from
// the slot it missed: an app that slept through five slots fires once, then
// waits for the next one.
type Recurrence struct {
	Kind      string `json:"kind"`                  // once | interval | weekly
	EveryMin  int    `json:"every_min,omitempty"`   // interval, >= MinIntervalMin
	Weekdays  []int  `json:"weekdays,omitempty"`    // weekly: time.Weekday (0 = Sunday); empty = every day
	TimeOfDay string `json:"time_of_day,omitempty"` // weekly: "HH:MM" local
}

// MinIntervalMin is the shortest interval: a recurring prompt starts a turn
// that costs tokens, and a tighter loop is what `/loop` is for.
const MinIntervalMin = 15

func (r Recurrence) Recurring() bool { return r.Kind == "interval" || r.Kind == "weekly" }

// Validate rejects a rule NextDue cannot schedule.
func (r Recurrence) Validate() error {
	switch r.Kind {
	case "", "once":
		return nil
	case "interval":
		if r.EveryMin < MinIntervalMin {
			return fmt.Errorf("interval must be at least %dm", MinIntervalMin)
		}
		return nil
	case "weekly":
		if _, _, ok := parseClock(r.TimeOfDay); !ok {
			return fmt.Errorf("bad time of day %q (use HH:MM)", r.TimeOfDay)
		}
		for _, d := range r.Weekdays {
			if d < 0 || d > 6 {
				return fmt.Errorf("bad weekday %d", d)
			}
		}
		return nil
	}
	return fmt.Errorf("unknown schedule kind %q (once, interval, weekly)", r.Kind)
}

// NextDue is the first run strictly after `from`, in loc for a weekly rule;
// false for a one-shot or an invalid rule. An interval advances elapsed time
// (60 minutes stay 60 minutes across a DST change); a weekly HH:MM that a
// spring-forward skips lands where time.Date normalises it, and one a
// fall-back repeats fires once.
func NextDue(r Recurrence, from time.Time, loc *time.Location) (time.Time, bool) {
	if r.Validate() != nil {
		return time.Time{}, false
	}
	switch r.Kind {
	case "interval":
		return from.Add(time.Duration(r.EveryMin) * time.Minute), true
	case "weekly":
		h, m, _ := parseClock(r.TimeOfDay)
		f := from.In(loc)
		for offset := 0; offset <= 7; offset++ {
			c := time.Date(f.Year(), f.Month(), f.Day()+offset, h, m, 0, 0, loc)
			if !c.After(from) || (len(r.Weekdays) > 0 && !slices.Contains(r.Weekdays, int(c.Weekday()))) {
				continue
			}
			return c, true
		}
	}
	return time.Time{}, false
}

var weekdayNames = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// Describe is the rule in words, for schedule_list and the UI.
func (r Recurrence) Describe() string {
	switch r.Kind {
	case "interval":
		if r.EveryMin%60 == 0 {
			return fmt.Sprintf("every %dh", r.EveryMin/60)
		}
		return fmt.Sprintf("every %dm", r.EveryMin)
	case "weekly":
		days := slices.Clone(r.Weekdays)
		slices.Sort(days)
		days = slices.Compact(days)
		var when string
		switch {
		case len(days) == 0 || len(days) == 7:
			when = "daily"
		case slices.Equal(days, []int{1, 2, 3, 4, 5}):
			when = "weekdays"
		default:
			names := make([]string, len(days))
			for i, d := range days {
				names[i] = strings.ToUpper(weekdayNames[d][:1]) + weekdayNames[d][1:]
			}
			when = strings.Join(names, ", ")
		}
		return when + " at " + r.TimeOfDay
	}
	return "once"
}

// ParseWeekly reads "mon,wed 09:00", "daily 9:30" or "weekdays 18:00".
func ParseWeekly(s string) (Recurrence, error) {
	fields := strings.Fields(strings.ToLower(s))
	if len(fields) != 2 {
		return Recurrence{}, fmt.Errorf("bad weekly %q (use e.g. \"mon,wed 09:00\" or \"daily 09:00\")", s)
	}
	h, m, ok := parseClock(fields[1])
	if !ok {
		return Recurrence{}, fmt.Errorf("bad time of day %q (use HH:MM)", fields[1])
	}
	r := Recurrence{Kind: "weekly", TimeOfDay: fmt.Sprintf("%02d:%02d", h, m)}
	switch fields[0] {
	case "daily":
	case "weekdays":
		r.Weekdays = []int{1, 2, 3, 4, 5}
	default:
		for _, name := range strings.Split(fields[0], ",") {
			i := slices.IndexFunc(weekdayNames, func(w string) bool { return len(name) >= 3 && strings.HasPrefix(name, w) })
			if i < 0 {
				return Recurrence{}, fmt.Errorf("bad weekday %q (use mon, tue, wed, thu, fri, sat, sun)", name)
			}
			if !slices.Contains(r.Weekdays, i) {
				r.Weekdays = append(r.Weekdays, i)
			}
		}
		slices.Sort(r.Weekdays)
	}
	return r, nil
}

func parseClock(s string) (h, m int, ok bool) {
	hs, ms, found := strings.Cut(strings.TrimSpace(s), ":")
	if !found || len(ms) != 2 {
		return 0, 0, false
	}
	h, err1 := strconv.Atoi(hs)
	m, err2 := strconv.Atoi(ms)
	return h, m, err1 == nil && err2 == nil && h >= 0 && h < 24 && m >= 0 && m < 60
}
