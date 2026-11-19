package app

import (
	"os"
	"testing"
)

func TestScreenshotChangesStatusFilters(t *testing.T) {
	directory := os.Getenv("MYGO_SHOTS")
	if directory == "" {
		t.Skip("set MYGO_SHOTS to a directory to write the filter preview")
	}
	h := menuTestApp(t)
	h.app.reviewedFiles = map[string]bool{"one.txt": true}
	h.tester.SetDark(true)
	openChangesFilterPopover(t, h)
	h.tester.Frame()
	h.tester.Move(0, 0)
	saveScreenshot(t, directory, "changes-file-filters-dark", h.tester)

	if err := h.tester.Click("Show only staged files"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	saveScreenshot(t, directory, "changes-staged-filter-dark", h.tester)
}
