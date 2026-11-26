package i18n

import (
	"fmt"
	"time"
)

var shortMonthNames = map[string][12]string{
	"es": {"ene.", "feb.", "mar.", "abr.", "may.", "jun.", "jul.", "ago.", "sept.", "oct.", "nov.", "dic."},
	"fr": {"janv.", "févr.", "mars", "avr.", "mai", "juin", "juil.", "août", "sept.", "oct.", "nov.", "déc."},
}

// MonthLabel returns a compact month name for charts and other dense UI.
func MonthLabel(t time.Time, lang string) string {
	lang = Normalize(lang)
	switch lang {
	case "zh-Hans", "zh-Hant", "ja":
		return fmt.Sprintf("%d月", t.Month())
	}
	if months, ok := shortMonthNames[lang]; ok {
		return months[int(t.Month())-1]
	}
	return t.Format("Jan")
}

// DateTimeLabel formats a date and time without a year for recent activity.
func DateTimeLabel(t time.Time, lang string) string {
	lang = Normalize(lang)
	clock := t.Format("15:04")
	switch lang {
	case "zh-Hans", "zh-Hant", "ja":
		return fmt.Sprintf("%d月%d日 %s", t.Month(), t.Day(), clock)
	case "es", "fr":
		return fmt.Sprintf("%d %s %s", t.Day(), MonthLabel(t, lang), clock)
	default:
		return t.Format("Jan 2 15:04")
	}
}

// FullDateTimeLabel formats a date with a year and time for commit details.
func FullDateTimeLabel(t time.Time, lang string) string {
	lang = Normalize(lang)
	clock := t.Format("15:04")
	switch lang {
	case "zh-Hans", "zh-Hant", "ja":
		return fmt.Sprintf("%d年%d月%d日 · %s", t.Year(), t.Month(), t.Day(), clock)
	case "es", "fr":
		return fmt.Sprintf("%d %s %d · %s", t.Day(), MonthLabel(t, lang), t.Year(), clock)
	default:
		return t.Format("Jan 2, 2006 · 15:04")
	}
}
