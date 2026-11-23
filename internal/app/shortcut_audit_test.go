package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func TestShortcutAuditDoesNotFireWhileBuildingViews(t *testing.T) {
	h := menuTestApp(t)
	h.app.attachRepositoryCommandLog(repository.NewCommandLog())
	for range 4 {
		h.tester.Frame()
	}
	if h.tester.HasText("Open the chosen item") {
		t.Fatal("a submit acknowledgement appeared without user input")
	}
	for _, entry := range h.app.commandLogEntries() {
		if entry.Shortcut != nil {
			t.Fatalf("building a view fabricated a keyboard invocation: %+v", entry)
		}
	}
	for _, toast := range h.app.pendingToasts {
		if toast.kind == "shortcut" {
			t.Fatal("building a view fabricated a shortcut notification")
		}
	}
}

func TestNativeListShortcutIsRecordedOnce(t *testing.T) {
	h := menuTestApp(t)
	h.app.attachRepositoryCommandLog(repository.NewCommandLog())
	h.tester.Key(0, ui.KeyDown)
	h.tester.Frame()
	entries := h.app.commandLogEntries()
	seen := []repository.ShortcutInvocation{}
	for _, entry := range entries {
		if entry.Shortcut != nil {
			seen = append(seen, *entry.Shortcut)
		}
	}
	if len(seen) != 1 {
		t.Fatalf("one native Down produced %d shortcuts: %+v", len(seen), seen)
	}
	if seen[0].Chord != "Down" || seen[0].Label != "Move through a list" {
		t.Fatalf("native shortcut lost its provenance: %+v", seen[0])
	}
	h.tester.Key(0, ui.KeyDown)
	h.tester.Frame()
	count := 0
	for _, entry := range h.app.commandLogEntries() {
		if entry.Shortcut != nil {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("two native Down inputs produced %d shortcuts, want 2", count)
	}
}

func TestCommitFormSubmitUsesVerifiedActionMetadata(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.attachRepositoryCommandLog(repository.NewCommandLog())
	h.app.showCommitForm = true
	h.tester = ui.NewTester(func(c *ui.Context) {
		h.app.installShortcutObserver(c)
		h.app.commitForm(c)
	}, 480, 260)
	if err := h.tester.Click("Summary"); err != nil {
		t.Fatalf("focus commit summary: %v; texts=%q", err, h.tester.Texts())
	}
	h.app.commitSummary = "verify keyboard submit"
	h.tester.Frame()
	h.tester.Key(0, ui.KeyEnter)
	h.tester.Frame()
	h.waitFor(t, "the form submission", func() bool { return calls.has("commit:verify keyboard submit") })
	var invocation *repository.ShortcutInvocation
	for _, entry := range h.app.commandLogEntries() {
		if entry.Shortcut != nil && entry.Shortcut.ID == "changes/commit-staged" {
			invocation = entry.Shortcut
			break
		}
	}
	if invocation == nil || invocation.Chord != "Enter" || invocation.Label != "Commit the staged changes" {
		t.Fatalf("submit provenance = %+v, want the commit's semantic action", invocation)
	}
}

func TestCustomShortcutRecordsItsCommandAndAcknowledgement(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.app.attachRepositoryCommandLog(repository.NewCommandLog())
	h.app.snapshot.Root = t.TempDir()
	h.app.settings.CustomCommands = []customCommandSetting{{
		Name: "custom refresh key", Command: "touch custom-f5", Key: "f5", Contexts: []string{"everywhere"},
	}}
	// Shortcut notifications are off by default; this test checks the toast.
	h.app.settings.DisableShortcutToasts = false
	h.app.settings.ShortcutToastsConfigured = true
	h.press(t, ui.KeyF5)
	marker := filepath.Join(h.app.snapshot.Root, "custom-f5")
	h.waitFor(t, "F5's custom command", func() bool {
		_, err := os.Stat(marker)
		return err == nil
	})

	if !h.tester.HasText("F5") {
		t.Fatal("the accepted shortcut did not acknowledge its invocation")
	}

	openUserCommandLogForTest(t, h.app, h.tester)
	h.tester.Frame()
	if !h.tester.HasText("F5") {
		t.Fatalf("Command Log does not name the shortcut: %q", h.texts())
	}
	if !h.tester.HasText("custom refresh key") {
		t.Fatalf("Command Log does not name the action: %q", h.texts())
	}
	copied := formatCommandLog(h.app.commandLogEntries())
	if !strings.Contains(copied, "F5") || !strings.Contains(copied, "custom refresh key") {
		t.Fatalf("copied log loses shortcut provenance: %q", copied)
	}
	keyboardEntries := 0
	for _, entry := range h.app.commandLogEntries() {
		if entry.Shortcut != nil {
			keyboardEntries++
		}
	}
	if keyboardEntries != 1 {
		t.Fatalf("one custom F5 produced %d audit records, want 1", keyboardEntries)
	}

}

func TestBuiltInShortcutLogsGlobalRefresh(t *testing.T) {
	h := menuTestApp(t)
	h.app.attachRepositoryCommandLog(repository.NewCommandLog())
	h.press(t, ui.KeyF5)
	openUserCommandLogForTest(t, h.app, h.tester)
	h.tester.Frame()
	if !h.tester.HasText("F5") || !h.tester.HasText("Refresh the repository") {
		t.Fatalf("the global refresh key was not recorded: %q", h.texts())
	}
}

func TestShortcutNotificationPreferenceDoesNotSilenceAuditOrResults(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.app.attachRepositoryCommandLog(repository.NewCommandLog())
	h.app.openSettings()
	h.tester.Frame()
	if err := h.tester.Click("Behavior"); err != nil {
		t.Fatalf("open Behavior settings: %v", err)
	}
	h.tester.Frame()
	control, ok := h.tester.Access("Shortcut notifications")
	if !ok {
		t.Fatal("Settings has no shortcut-notification preference")
	}
	if control.Checked {
		t.Fatal("shortcut notifications should be disabled by default, including for older settings files")
	}
	if err := h.tester.Click("Shortcut notifications"); err != nil {
		t.Fatalf("enable shortcut notifications: %v", err)
	}
	h.tester.Frame()
	if h.app.settings.DisableShortcutToasts {
		t.Fatal("the notification preference did not persist into app settings")
	}
	if !h.app.settings.ShortcutToastsConfigured {
		t.Fatal("the toggle did not mark the preference configured")
	}
	control, ok = h.tester.Access("Shortcut notifications")
	if !ok || !control.Checked {
		t.Fatalf("the notification switch did not turn on: %+v", control)
	}
	h.app.closeSettings()

	h.app.snapshot.Root = t.TempDir()
	h.app.settings.CustomCommands = []customCommandSetting{{
		Name: "logged even when quiet", Command: "touch quiet-f5", Key: "f5", Contexts: []string{"everywhere"},
	}}
	h.press(t, ui.KeyF5)
	h.waitFor(t, "the quiet custom command", func() bool {
		_, err := os.Stat(filepath.Join(h.app.snapshot.Root, "quiet-f5"))
		return err == nil
	})
	if !h.tester.HasText("F5") {
		t.Fatal("an opted-in shortcut acknowledgement did not show")
	}

	openUserCommandLogForTest(t, h.app, h.tester)
	h.tester.Frame()
	if !h.tester.HasText("F5") || !h.tester.HasText("logged even when quiet") {
		t.Fatalf("opting out hid the shortcut audit: %q", h.texts())
	}
	h.app.notify("error", "Operation result remains visible", "ordinary notification")
	h.tester.Frame()
	if !h.tester.HasText("Operation result remains visible") {
		t.Fatal("the shortcut preference suppressed an operation-level notification")
	}
}

// A fresh app launch has shortcut notifications off, command-level audit
// intact, and an acknowledgement that never appears.
func TestShortcutNotificationDefaultOffDoesNotAcknowledge(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.app.attachRepositoryCommandLog(repository.NewCommandLog())
	if !h.app.settings.DisableShortcutToasts || !h.app.settings.ShortcutToastsConfigured {
		t.Fatal("shortcut notifications must be disabled by default")
	}
	root := t.TempDir()
	h.app.snapshot.Root = root
	h.app.settings.CustomCommands = []customCommandSetting{{
		Name: "custom refresh key", Command: "touch default-off-f5", Key: "f5", Contexts: []string{"everywhere"},
	}}
	h.press(t, ui.KeyF5)
	h.waitFor(t, "the quiet custom command", func() bool {
		_, err := os.Stat(filepath.Join(root, "default-off-f5"))
		return err == nil
	})
	if h.tester.HasText("F5") {
		t.Fatal("the default launch acknowledges shortcuts")
	}
	openUserCommandLogForTest(t, h.app, h.tester)

	if !h.tester.HasText("F5") || !h.tester.HasText("custom refresh key") {
		t.Fatalf("the audit log lost the shortcut: %q", h.texts())
	}
}
