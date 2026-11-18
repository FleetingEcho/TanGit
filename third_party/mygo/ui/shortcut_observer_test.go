package ui_test

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func shortcutTestList() (*ui.Tester, *[]ui.ShortcutEvent, *int) {
	selected := 0
	state := &ui.ListState{Selected: &selected}
	events := []ui.ShortcutEvent{}
	view := func(c *ui.Context) {
		c.ObserveShortcuts(func(event ui.ShortcutEvent) { events = append(events, event) })
		names := []string{"one", "two", "three"}
		ui.List(c, state, len(names)).Label("Fruit list").Rows(func(row ui.ListRow) {
			ui.Text(row.Context, names[row.Index])
		})
	}
	return ui.NewTester(view, 400, 260), &events, &selected
}

func TestShortcutObserverReportsNativeAcceptedActionOnce(t *testing.T) {
	tt, events, selected := shortcutTestList()
	if err := tt.Click("one"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(*events) != 0 {
		t.Fatalf("a pointer click produced a keyboard event: %+v", *events)
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if len(*events) != 1 {
		t.Fatalf("Delivered keyboard events = %d, want one: %+v", len(*events), *events)
	}
	event := (*events)[0]
	if event.Key != ui.KeyDown || event.Receiver != "Fruit list" || event.AlreadyRecorded {
		t.Fatalf("native event = %+v", event)
	}
	if *selected != 1 {
		t.Fatalf("observer changed native selection: %d", *selected)
	}
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if len(*events) != 2 || *selected != 0 {
		t.Fatalf("second native action was lost or duplicated: events=%d selected=%d", len(*events), *selected)
	}
}

func TestShortcutObserverIsNilByDefault(t *testing.T) {
	selected := 0
	state := &ui.ListState{Selected: &selected}
	view := func(c *ui.Context) {
		names := []string{"one", "two"}
		ui.List(c, state, len(names)).Label("Unobserved").Rows(func(row ui.ListRow) {
			ui.Text(row.Context, names[row.Index])
		})
	}
	tt := ui.NewTester(view, 400, 200)
	if err := tt.Click("one"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if selected != 1 {
		t.Fatalf("default MyGo no longer navigates normally: %d", selected)
	}
}

func TestShortcutAnnotationPreservesNoDuplicateLogging(t *testing.T) {
	ownLog := 0
	observerLog := 0
	view := func(c *ui.Context) {
		c.ObserveShortcuts(func(event ui.ShortcutEvent) {
			if event.AlreadyRecorded {
				return
			}
			if event.ActionID != "demo/action" || event.Label != "Do action" {
				t.Errorf("annotation was lost: %+v", event)
			}
			observerLog++
		})
		button := ui.Button(c, "Do action")
		button.DescribeShortcut(ui.ShortcutDescriptor{ID: "demo/action", Label: "Do action", AlreadyRecorded: true}, 0, ui.KeySpace)
		button.OnShortcut(0, ui.KeySpace, func() { ownLog++ })
	}
	tt := ui.NewTester(view, 300, 160)
	if err := tt.Click("Do action"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	if ownLog != 1 || observerLog != 0 {
		t.Fatalf("same invocation recorded twice: app=%d observer=%d", ownLog, observerLog)
	}
}

func TestShortcutObserverSubmitFiresOnlyOnSubmit(t *testing.T) {
	var submitted bool
	events := []ui.ShortcutEvent{}
	text := "draft"
	view := func(c *ui.Context) {
		c.ObserveShortcuts(func(event ui.ShortcutEvent) { events = append(events, event) })
		ui.TextInput(c, &text).Label("Summary").AutoFocus().OnSubmit(func() { submitted = true })
	}
	tt := ui.NewTester(view, 400, 160)
	tt.Frame()
	if len(events) != 0 {
		t.Fatalf("constructing the form submitted it: %+v", events)
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if !submitted || len(events) != 1 {
		t.Fatalf("submit was not delivered once: submitted=%v events=%v", submitted, events)
	}
	if events[0].Key != ui.KeyEnter || events[0].Receiver != "Summary" {
		t.Fatalf("submit metadata = %+v", events[0])
	}
}
