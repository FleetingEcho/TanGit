package app

import (
	"fmt"
	"hash/fnv"
	"math"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// languageColors are the familiar colors of the common languages.
var languageColors = map[string]string{
	"Go": "#00ADD8", "Python": "#3572A5", "JavaScript": "#F1E05A", "TypeScript": "#3178C6", "Rust": "#DEA584",
	"Ruby": "#CC342D", "PHP": "#4F5D95", "Java": "#B07219", "Kotlin": "#A97BFF", "Scala": "#C22D40",
	"Swift": "#F05138", "Objective-C": "#438EFF", "C": "#6E6E6E", "C++": "#F34B7D", "C#": "#178600",
	"Dart": "#00B4AB", "Lua": "#000080", "Shell": "#89E051", "HTML": "#E34C26", "CSS": "#563D7C", "SCSS": "#C6538C",
	"Vue": "#41B883", "Svelte": "#FF3E00", "SQL": "#E38C00", "Markdown": "#6B7280", "YAML": "#CB171E",
	"JSON": "#8B8B8B", "TOML": "#9C4221", "Makefile": "#427819", "Dockerfile": "#384D54", "Elixir": "#6E4A7E",
	"Haskell": "#5E5086", "Zig": "#EC915C", "HCL": "#844FBA", "Protocol Buffers": "#5C7C8A",
}

// languageColor is the color of a language: its familiar one, else a steady
// shade picked from its name.
func languageColor(name string) ui.Color {
	if hex, ok := languageColors[name]; ok {
		return ui.Hex(hex)
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(name))
	sum := hash.Sum32()
	return ui.RGB(uint8(90+sum%120), uint8(90+(sum>>8)%120), uint8(90+(sum>>16)%120))
}

// languageSlice is one wedge of the chart: a language, or the rest together.
type languageSlice struct {
	Name  string
	Bytes int64
	Files int
	Other bool
}

// languageSlices keeps the biggest languages and folds the rest into "Other".
func languageSlices(stats repository.LanguageStats, keep int) []languageSlice {
	var slices []languageSlice
	var other languageSlice
	other.Name, other.Other = "Other", true
	for index, share := range stats.Languages {
		if index < keep {
			slices = append(slices, languageSlice{Name: share.Name, Bytes: share.Bytes, Files: share.Files})
			continue
		}
		other.Bytes += share.Bytes
		other.Files += share.Files
	}
	if other.Bytes > 0 {
		slices = append(slices, other)
	}
	return slices
}

func languagePercent(part, total int64) string {
	if total <= 0 {
		return "0%"
	}
	value := float64(part) * 100 / float64(total)
	if value < 0.1 {
		return "<0.1%"
	}
	return fmt.Sprintf("%.1f%%", value)
}

// wedgePath is the ring segment between two angles, in radians clockwise from
// the top.
func wedgePath(cx, cy, outer, inner, from, to float64) *ui.Path {
	var path ui.Path
	steps := max(2, int(math.Ceil((to-from)/(math.Pi/60))))
	point := func(radius, angle float64) (float32, float32) {
		return float32(cx + radius*math.Sin(angle)), float32(cy - radius*math.Cos(angle))
	}
	for step := 0; step <= steps; step++ {
		x, y := point(outer, from+(to-from)*float64(step)/float64(steps))
		if step == 0 {
			path.MoveTo(x, y)
		} else {
			path.LineTo(x, y)
		}
	}
	for step := steps; step >= 0; step-- {
		x, y := point(inner, from+(to-from)*float64(step)/float64(steps))
		path.LineTo(x, y)
	}
	return path.Close()
}

// cockpitLanguagesCard shows what the repository's source is written in, as a
// ring with a legend of each language's share.
func (a *app) cockpitLanguagesCard(c *ui.Context) {
	insights := a.cockpit.insights
	if insights.langUnavailable {
		return
	}
	a.cockpitCard(c, "languages", "Languages", fileCodeIcon, func() {
		theme := c.Theme()
		switch {
		case insights.langErr != "":
			ui.Text(c, tr("Could not read the languages")).Bold().FontSize(theme.FontSize - 1)
			ui.Text(c, insights.langErr).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
			return
		case !insights.langLoaded:
			cockpitLoading(c, tr("Reading the files…"), 130)
			return
		case len(insights.languages.Languages) == 0:
			ui.Text(c, tr("No source files found")).TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
			return
		}
		stats := insights.languages
		slices := languageSlices(stats, 7)
		colorOf := func(slice languageSlice) ui.Color {
			if slice.Other {
				return theme.TextMuted.Alpha(0.5)
			}
			return languageColor(slice.Name)
		}
		const size = 118
		ui.Row(c).FillWidth().MinWidth(0).Grow(1).Gap(18).Center().Children(func() {
			ui.Box(c).Size(size, size).Shrink(0).Label(tr("Languages")).Draw(func(p *ui.Painter, r ui.Rect) {
				cx, cy := float64(r.X+r.W/2), float64(r.Y+r.H/2)
				outer := float64(min(r.W, r.H)) / 2
				angle := 0.0
				for _, slice := range slices {
					sweep := 2 * math.Pi * float64(slice.Bytes) / float64(stats.TotalBytes)
					// A hair of gap keeps neighbours apart without hiding tiny wedges.
					gap := math.Min(0.02, sweep/4)
					p.FillPath(wedgePath(cx, cy, outer, outer*0.58, angle, angle+sweep-gap), colorOf(slice))
					angle += sweep
				}
			})
			ui.Column(c).Width(0).Grow(1).MinWidth(0).MaxWidthPercent(70).Gap(2).Children(func() {
				for _, slice := range slices {
					slice := slice
					c.Key("language:" + slice.Name)
					tip := slice.Name + " · " + trf("%d files", slice.Files) + " · " + formatBytes(slice.Bytes)
					ui.Row(c).FillWidth().MinWidth(0).AlignItems(ui.Center).Gap(7).Padding(1, 2).Tooltip(tip).Children(func() {
						ui.Box(c).Size(9, 9).Radius(5).Shrink(0).Background(colorOf(slice))
						ui.Text(c, tr(slice.Name)).SingleLine().Width(0).Grow(1).MinWidth(0)
						ui.Text(c, languagePercent(slice.Bytes, stats.TotalBytes)).FontSize(theme.FontSize - 1).
							TextColor(theme.TextMuted).SingleLine().Shrink(0)
					})
				}
			})
		})
	})
}

// formatBytes writes a size the way a person reads it: 1.4 MB.
func formatBytes(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value, suffix := float64(n), "kMGT"
	index := -1
	for value >= unit && index < len(suffix)-1 {
		value /= unit
		index++
	}
	return fmt.Sprintf("%.1f %cB", value, suffix[index])
}
