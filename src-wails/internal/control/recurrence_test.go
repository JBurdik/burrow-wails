package control

import (
	"testing"
	"time"
)

func TestNextDue(t *testing.T) {
	prague, err := time.LoadLocation("Europe/Prague")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, prague)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	weekly := func(spec string) Recurrence {
		r, err := ParseWeekly(spec)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	cases := []struct {
		name string
		rule Recurrence
		from time.Time
		want time.Time // zero = none
	}{
		{"once never recurs", Recurrence{Kind: "once"}, at("2026-10-30 10:00"), time.Time{}},
		{"interval below the minimum is refused", Recurrence{Kind: "interval", EveryMin: 5}, at("2026-10-30 10:00"), time.Time{}},
		{"interval adds elapsed time", Recurrence{Kind: "interval", EveryMin: 90}, at("2026-10-30 10:00"), at("2026-10-30 11:30")},
		// 2026-03-29 02:00 CET → 03:00 CEST: one wall-clock hour is gone.
		{"interval across spring-forward stays 60 elapsed minutes", Recurrence{Kind: "interval", EveryMin: 60}, at("2026-03-29 01:30"), at("2026-03-29 01:30").Add(time.Hour)},
		// Catch-up: computed from when the run fires, so a week's sleep is one
		// run, and the next is a full interval later — never in the past.
		{"interval after a long sleep is one interval from now", Recurrence{Kind: "interval", EveryMin: 60}, at("2026-11-06 09:17"), at("2026-11-06 10:17")},
		{"weekly later today", weekly("fri 18:00"), at("2026-10-30 10:00"), at("2026-10-30 18:00")},
		{"weekly exactly at the slot is the next one", weekly("fri 18:00"), at("2026-10-30 18:00"), at("2026-11-06 18:00")},
		{"weekly wraps past Sunday", weekly("mon,wed 09:00"), at("2026-10-30 10:00"), at("2026-11-02 09:00")},
		{"weekly picks the nearest listed day", weekly("mon,wed 09:00"), at("2026-11-02 09:30"), at("2026-11-04 09:00")},
		{"daily", weekly("daily 07:05"), at("2026-10-30 23:59"), at("2026-10-31 07:05")},
		{"weekdays skip the weekend", weekly("weekdays 09:00"), at("2026-10-30 12:00"), at("2026-11-02 09:00")},
		{"weekly after a long sleep is the next slot, not the missed ones", weekly("mon 09:00"), at("2026-12-01 08:00"), at("2026-12-07 09:00")},
		// 2026-10-25 03:00 CEST → 02:00 CET: 02:30 happens twice; once is enough.
		{"fall-back: the repeated hour fires once", weekly("sun 02:30"), at("2026-10-25 02:30").Add(time.Hour), at("2026-11-01 02:30")},
	}
	for _, c := range cases {
		got, ok := NextDue(c.rule, c.from, prague)
		if ok != !c.want.IsZero() || (ok && !got.Equal(c.want)) {
			t.Errorf("%s: got %v (%v), want %v", c.name, got, ok, c.want)
		}
	}

	// Spring-forward: 02:30 does not exist on 2026-03-29. It still fires that
	// day, after `from`, and only once — the next run is a week later.
	r := weekly("sun 02:30")
	first, ok := NextDue(r, at("2026-03-28 12:00"), prague)
	if !ok || first.In(prague).Day() != 29 || !first.After(at("2026-03-28 12:00")) {
		t.Fatalf("spring-forward: got %v", first)
	}
	second, _ := NextDue(r, first, prague)
	if want := at("2026-04-05 02:30"); !second.Equal(want) {
		t.Fatalf("after spring-forward: got %v, want %v", second, want)
	}
}

func TestParseWeekly(t *testing.T) {
	for _, bad := range []string{"", "mon", "mon 25:00", "funday 09:00", "mon 9:5"} {
		if _, err := ParseWeekly(bad); err == nil {
			t.Errorf("ParseWeekly(%q) accepted", bad)
		}
	}
	r, err := ParseWeekly("Wed,mon,monday 9:00")
	if err != nil || r.TimeOfDay != "09:00" || len(r.Weekdays) != 2 || r.Weekdays[0] != 1 || r.Weekdays[1] != 3 {
		t.Fatalf("got %+v, %v", r, err)
	}
	if got := r.Describe(); got != "Mon, Wed at 09:00" {
		t.Fatalf("Describe: %q", got)
	}
}
