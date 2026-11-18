package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func TestCustomShortcutRecordsItsCommandAndAcknowledgement(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.app.attachRepositoryCommandLog(repository.NewCommandLog())
	h.app.snapshot.Root = t.TempDir()
	h.app.settings.CustomCommands = []customCommandSetting{{
		Name: "custom refresh key", Command: "touch custom-f5", Key: "f5", Contexts: []string{"everywhere"},
	}}
	h.press(t, ui.KeyF5)
	marker := filepath.Join(h.app.snapshot.Root, "custom-f5")
	h.waitFor(t, "F5's custom command", func() bool {
		_, err := os.Stat(marker)
		return err == nil
	})

	if !h.tester.HasText("F5") {
		t.Fatal("the accepted shortcut did not acknowledge its invocation")
	}

	h.app.openCommandLog()
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

}

func TestBuiltInShortcutLogsGlobalRefresh(t *testing.T) {
	h := menuTestApp(t)
	h.app.attachRepositoryCommandLog(repository.NewCommandLog())
	h.press(t, ui.KeyF5)
	h.app.openCommandLog()
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
	if !control.Checked {
		t.Fatal("shortcut notifications should be enabled for older/default settings")
	}
	if err := h.tester.Click("Shortcut notifications"); err != nil {
		t.Fatalf("disable shortcut notifications: %v", err)
	}
	h.tester.Frame()
	if !h.app.settings.DisableShortcutToasts {
		t.Fatal("the notification preference did not persist into app settings")
	}
	control, ok = h.tester.Access("Shortcut notifications")
	if !ok || control.Checked {
		t.Fatalf("the notification switch did not turn off: %+v", control)
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
	if h.tester.HasText("F5") {
		t.Fatal("a shortcut acknowledgement remained visible after the opt-out")
	}

	h.app.openCommandLog()
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
