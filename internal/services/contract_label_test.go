package services

import (
	"testing"
	"time"
)

func TestParseShortNameMonth(t *testing.T) {
	cases := []struct {
		in    string
		month time.Month
		year  int
		ok    bool
	}{
		{"Crude Oil Nov 26", time.November, 2026, true},
		{"Natural Gas Nov 26", time.November, 2026, true},
		{"RBOB Gasoline Dec 26", time.December, 2026, true},
		{"Heating Oil Jan 27", time.January, 2027, true},
		{"Brent Crude Oil Last Day Financ", 0, 0, false},
		{"Mayan Crude 26", 0, 0, false},     // "May" inside a word must not match
		{"Crude Oil Nov 2026", 0, 0, false}, // 4-digit years are not the Yahoo format
		{"", 0, 0, false},
	}
	for _, c := range cases {
		m, y, ok := parseShortNameMonth(c.in)
		if ok != c.ok || m != c.month || y != c.year {
			t.Errorf("parseShortNameMonth(%q) = (%v, %d, %v), want (%v, %d, %v)", c.in, m, y, ok, c.month, c.year, c.ok)
		}
	}
}

// Brent (ICE B / NYMEX BZ) expires on the last business day of the second
// month before delivery, so the front month is two or three months ahead.
func TestBrentFrontMonth(t *testing.T) {
	ny := nyTZ
	cases := []struct {
		at    time.Time
		month time.Month
		year  int
	}{
		// 2026-10-16: the day prod showed "Oct 2026 Contract". BZ=F matched
		// BZZ26.NYM (Dec 2026) on Yahoo that day.
		{time.Date(2026, 10, 16, 14, 0, 0, 0, ny), time.December, 2026},
		// Fri 2026-10-30 is the last weekday of Oct = Dec contract expiry day.
		{time.Date(2026, 10, 30, 12, 0, 0, 0, ny), time.December, 2026},
		// Late evening ET on expiry day is still that exchange day even
		// though it's already Oct 31 in UTC.
		{time.Date(2026, 10, 31, 2, 0, 0, 0, time.UTC), time.December, 2026},
		{time.Date(2026, 10, 31, 12, 0, 0, 0, ny), time.January, 2027},
		{time.Date(2026, 11, 2, 9, 0, 0, 0, ny), time.January, 2027},
		{time.Date(2026, 11, 30, 9, 0, 0, 0, ny), time.January, 2027},
		{time.Date(2026, 12, 1, 9, 0, 0, 0, ny), time.February, 2027},
		{time.Date(2026, 12, 31, 9, 0, 0, 0, ny), time.February, 2027},
		{time.Date(2027, 1, 1, 9, 0, 0, 0, ny), time.March, 2027},
		// Jan 2027 ends on a Sunday, so the Mar contract expires Fri Jan 29.
		{time.Date(2027, 1, 29, 9, 0, 0, 0, ny), time.March, 2027},
		{time.Date(2027, 1, 30, 9, 0, 0, 0, ny), time.April, 2027},
	}
	for _, c := range cases {
		m, y := brentFrontMonth(c.at)
		if m != c.month || y != c.year {
			t.Errorf("brentFrontMonth(%s) = %v %d, want %v %d", c.at.Format(time.RFC3339), m, y, c.month, c.year)
		}
	}
}

func TestContractLabel(t *testing.T) {
	at := time.Date(2026, 10, 16, 20, 59, 0, 0, time.UTC)
	cases := []struct {
		sym, short, want string
	}{
		{"CL=F", "Crude Oil Nov 26", "Nov 2026 Contract"},
		{"NG=F", "Natural Gas Nov 26", "Nov 2026 Contract"},
		{"RB=F", "RBOB Gasoline Dec 26", "Dec 2026 Contract"},
		// Brent metadata has no month: computed from the expiry rule, not
		// from the calendar month (which produced the stale "Oct 2026").
		{"BZ=F", "Brent Crude Oil Last Day Financ", "Dec 2026 Contract"},
		// Unknown product without a month: no guess, UI says "Front month".
		{"XX=F", "Some Future", ""},
	}
	for _, c := range cases {
		if got := contractLabel(c.sym, c.short, at); got != c.want {
			t.Errorf("contractLabel(%q, %q) = %q, want %q", c.sym, c.short, got, c.want)
		}
	}
}
