package app

import (
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

func TestNotificationActionsShareOneLine(t *testing.T) {
	a := newApp(fakeOpener{})
	a.settingsLoaded = true
	a.notifyToast(appToast{kind: "success", title: "Download complete", description: "1 completed", action: "Reveal", moreActions: []appToastAction{{label: "Details"}}})
	tester := ui.NewTester(a.view, 800, 600)
	tester.Frame()
	reveal, ok := tester.Find("Reveal")
	if !ok {
		t.Fatal("Reveal action missing")
	}
	details, ok := tester.Find("Details")
	if !ok {
		t.Fatal("Details action missing")
	}
	if math.Abs(float64(reveal.Y-details.Y)) > 1 || details.X < reveal.X+reveal.W {
		t.Fatalf("actions are not on one line: Reveal=%+v Details=%+v", reveal, details)
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "notification-actions", tester)
	}
}

func TestDivergentPullNotificationActionsShareOneLine(t *testing.T) {
	a := newApp(fakeOpener{})
	a.settingsLoaded = true
	a.notifyError(fmt.Errorf("pull: %w", repository.ErrPullDiverged))
	tester := ui.NewTester(a.view, 800, 600)
	tester.Frame()
	previousRight := float32(-1)
	previousY := float32(-1)
	for _, label := range []string{"Merge", "Rebase", "View log"} {
		action, ok := tester.Find(label)
		if !ok {
			t.Fatalf("%s action missing: %q", label, tester.Texts())
		}
		if previousRight >= 0 && (math.Abs(float64(previousY-action.Y)) > 1 || action.X < previousRight) {
			t.Fatalf("divergent-pull actions do not share one line at %s: %+v", label, action)
		}
		previousRight = action.X + action.W
		previousY = action.Y
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "divergent-pull-actions", tester)
	}
}
