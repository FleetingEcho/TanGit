package app

import "testing"

// The commands page lists each command with its shortcut and scope, and shows
// an empty state with an add button when there are none.
func TestCommandsSettingsPageListsAndEmptyState(t *testing.T) {
	h := menuTestApp(t)
	h.app.openSettingsSection(settingsPageCommands)
	h.tester.SetSize(900, 700)
	h.tester.Frame()
	if _, ok := h.tester.Find("No commands yet"); !ok {
		t.Fatal("the empty state is missing")
	}
	h.app.settings.CustomCommands = []customCommandSetting{
		{Name: "gp", Command: "git pull origin main", Key: "ctrl+g", Contexts: []string{"changes"}},
	}
	h.tester.Frame()
	for _, want := range []string{"gp", "git pull origin main", "Changes"} {
		if _, ok := h.tester.Find(want); !ok {
			t.Errorf("%q is missing from the command row", want)
		}
	}
	if _, ok := h.tester.Find("No commands yet"); ok {
		t.Fatal("the empty state shows beside a command")
	}
	if h.app.crash != "" {
		t.Fatalf("the page crashed: %s", h.app.crash)
	}
}

// The "Active in" choices lead with Everywhere, show an icon for each page, and
// choosing a page narrows the command to it.
func TestCommandFormActiveInChips(t *testing.T) {
	if customCommandContexts[0].id != "everywhere" {
		t.Fatalf("Everywhere is not the first choice: %v", customCommandContexts[0])
	}
	for _, context := range customCommandContexts {
		if customCommandContextIcon(context.id) == nil {
			t.Errorf("%s has no icon", context.id)
		}
	}
	h := menuTestApp(t)
	h.app.openSettingsSection(settingsPageCommands)
	h.app.openCustomCommandForm(-1)
	h.tester.SetSize(900, 900)
	h.tester.Frame()
	if _, ok := h.tester.Find("Everywhere"); !ok {
		t.Fatal("the Everywhere chip is missing")
	}
	h.app.toggleCustomCommandFormContext("changes", true)
	if got := h.app.settingsUI.customCommandFormContexts; len(got) != 1 || got[0] != "changes" {
		t.Fatalf("contexts after choosing Changes = %v", got)
	}
	h.tester.Frame()
	if h.app.crash != "" {
		t.Fatalf("the form crashed: %s", h.app.crash)
	}
}
