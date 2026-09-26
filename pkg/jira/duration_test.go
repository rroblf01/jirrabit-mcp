package jira

import "testing"

func TestParseJiraDuration(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"30m", 30},
		{"1h", 60},
		{"1h 30m", 90},
		{"1h30m", 90},
		{"2d", 2880},
		{"1w", 10080},
		{"1w 2d 3h 4m", 10080 + 2880 + 180 + 4},
		{"90", 90}, // a bare number means minutes in Jira
		{"+2h", 120},
		{"  1h  30m  ", 90},
	}
	for _, c := range cases {
		got, err := ParseJiraDuration(c.in)
		if err != nil {
			t.Errorf("ParseJiraDuration(%q) returned error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseJiraDuration(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseJiraDurationRejects(t *testing.T) {
	bad := []string{
		"",
		"0m",    // not greater than zero
		"0",     // ditto
		"-30m",  // negative
		"1h30",  // trailing digits with no unit
		"h",     // unit with no number
		"1x",    // unknown unit
		"abc",   // not a duration at all
		"1h 30", // trailing digits
	}
	for _, in := range bad {
		if got, err := ParseJiraDuration(in); err == nil {
			t.Errorf("ParseJiraDuration(%q) = %d, want an error", in, got)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		minutes int
		want    string
	}{
		{0, "0m"},
		{5, "5m"},
		{60, "1h"},
		{90, "1h 30m"},
		{120, "2h"},
		{1440, "24h"},
	}
	for _, c := range cases {
		if got := formatDuration(c.minutes); got != c.want {
			t.Errorf("formatDuration(%d) = %q, want %q", c.minutes, got, c.want)
		}
	}
}
