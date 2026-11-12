package app

import (
	"math"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
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
