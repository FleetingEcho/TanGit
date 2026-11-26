package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSelectedRowKeepsAccentWhenWindowIsInactive(t *testing.T) {
	settings := []appSettings{
		{Theme: "Dracula"},
		{Theme: "System"},
		{Theme: customThemeValue("Blue custom"), CustomThemes: []namedCustomTheme{{
			Name: "Blue custom", customThemeSetting: customThemeSetting{
				Base: "dark", Colors: map[string]string{"accent": "#3584e4"},
			},
		}}},
	}
	for _, setting := range settings {
		t.Run(setting.Theme, func(t *testing.T) {
			theme := themeForSettings(setting, true)
			tt := ui.NewTester(func(c *ui.Context) {
				c.SetTheme(theme)
				ui.Box(c).Selected(true).Size(100, 30).Label("selected row")
			}, 120, 50)
			active := buttonInk(t, tt, "selected row", 0.5, 0.5)
			tt.SetWindowActive(false)
			inactive := buttonInk(t, tt, "selected row", 0.5, 0.5)
			if inactive != active {
				t.Fatalf("selected row lost its accent while unfocused: active=%v inactive=%v", active, inactive)
			}
			tt.SetWindowActive(true)
			focusedAgain := buttonInk(t, tt, "selected row", 0.5, 0.5)
			if focusedAgain != active {
				t.Fatalf("selected row changed after refocusing: active=%v again=%v", active, focusedAgain)
			}
		})
	}
}
