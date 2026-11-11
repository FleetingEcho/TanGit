package app

import (
	"math"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestRemoteExplorerDownloadStaysOnDirectoryRow(t *testing.T) {
	a, _ := newRemoteTransferUITestApp(t)
	a.code.rows = []fileTreeRow{
		{source: -1, path: "linuxuser/.ssh", name: ".ssh", depth: 1},
		{source: 0, path: "linuxuser/.bashrc", name: ".bashrc", depth: 1},
	}
	tester := ui.NewTester(func(c *ui.Context) { ui.Column(c).Width(280).Height(400).Children(func() { a.codeFileExplorer(c) }) }, 300, 420)
	tester.Frame()
	directory, ok := tester.Find("Collapse directory linuxuser/.ssh")
	if !ok {
		t.Fatalf("directory missing: %v", tester.Texts())
	}
	next, _ := tester.Find("Open linuxuser/.bashrc")
	tester.Move(directory.X+directory.W/2, directory.Y+directory.H/2)
	tester.Frame()
	download, ok := tester.Find("Download linuxuser/.ssh")
	if !ok {
		t.Fatalf("download action missing: %v", tester.Texts())
	}
	hovered, _ := tester.Find("Collapse directory linuxuser/.ssh")
	if math.Abs(float64(download.Y+download.H/2-hovered.Y-hovered.H/2)) > 1 {
		t.Fatalf("download is on another line: directory=%+v download=%+v", hovered, download)
	}
	if download.X < hovered.X+hovered.W {
		t.Fatalf("download is not after directory contents: directory=%+v download=%+v", hovered, download)
	}
	after, _ := tester.Find("Open linuxuser/.bashrc")
	if after.Y != next.Y {
		t.Fatalf("hover moved next row: before=%+v after=%+v", next, after)
	}
	tester.Move(after.X+after.W/2, after.Y+after.H/2)
	tester.Frame()
	fileDownload, ok := tester.Find("Download linuxuser/.bashrc")
	if !ok {
		t.Fatal("file download action missing")
	}
	file, _ := tester.Find("Open linuxuser/.bashrc")
	if math.Abs(float64(fileDownload.Y+fileDownload.H/2-file.Y-file.H/2)) > 1 || fileDownload.X+fileDownload.W > file.X+file.W || fileDownload.X < file.X+file.W-50 {
		t.Fatalf("file action is not at the row end: file=%+v download=%+v", file, fileDownload)
	}
	if file.H != next.H || file.Y != next.Y {
		t.Fatalf("file hover changed row bounds: before=%+v after=%+v", next, file)
	}
	tester.Move(hovered.X+hovered.W/2, hovered.Y+hovered.H/2)
	tester.Frame()
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "remote-explorer-download", tester)
	}
}
