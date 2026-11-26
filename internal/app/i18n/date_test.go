package i18n

import (
	"testing"
	"time"
)

func TestLocalizedDateLabels(t *testing.T) {
	at := time.Date(2006, time.January, 2, 15, 4, 0, 0, time.UTC)
	tests := []struct {
		lang         string
		month        string
		dateTime     string
		fullDateTime string
	}{
		{lang: "en", month: "Jan", dateTime: "Jan 2 15:04", fullDateTime: "Jan 2, 2006 · 15:04"},
		{lang: "zh-Hans", month: "1月", dateTime: "1月2日 15:04", fullDateTime: "2006年1月2日 · 15:04"},
		{lang: "zh-Hant", month: "1月", dateTime: "1月2日 15:04", fullDateTime: "2006年1月2日 · 15:04"},
		{lang: "ja", month: "1月", dateTime: "1月2日 15:04", fullDateTime: "2006年1月2日 · 15:04"},
		{lang: "es", month: "ene.", dateTime: "2 ene. 15:04", fullDateTime: "2 ene. 2006 · 15:04"},
		{lang: "fr", month: "janv.", dateTime: "2 janv. 15:04", fullDateTime: "2 janv. 2006 · 15:04"},
	}
	for _, test := range tests {
		t.Run(test.lang, func(t *testing.T) {
			if got := MonthLabel(at, test.lang); got != test.month {
				t.Errorf("MonthLabel() = %q, want %q", got, test.month)
			}
			if got := DateTimeLabel(at, test.lang); got != test.dateTime {
				t.Errorf("DateTimeLabel() = %q, want %q", got, test.dateTime)
			}
			if got := FullDateTimeLabel(at, test.lang); got != test.fullDateTime {
				t.Errorf("FullDateTimeLabel() = %q, want %q", got, test.fullDateTime)
			}
		})
	}
}

func TestDateLabelsNormalizeUnknownLanguage(t *testing.T) {
	at := time.Date(2026, time.December, 31, 23, 59, 0, 0, time.UTC)
	if got, want := DateTimeLabel(at, "unknown"), "Dec 31 23:59"; got != want {
		t.Fatalf("DateTimeLabel() = %q, want %q", got, want)
	}
	if got, want := MonthLabel(at, ""), "Dec"; got != want {
		t.Fatalf("MonthLabel() = %q, want %q", got, want)
	}
}
