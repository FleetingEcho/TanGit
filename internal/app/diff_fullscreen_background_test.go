package app

import (
	"context"
	"fmt"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

type fullscreenBackgroundDiffSession struct {
	fakeSession
	secondStarted chan struct{}
	secondRelease chan struct{}
}

func (s *fullscreenBackgroundDiffSession) Diff(ctx context.Context, file repository.File, options repository.DiffOptions) (repository.Diff, error) {
	if file.Path == "b.txt" {
		select {
		case s.secondStarted <- struct{}{}:
		default:
		}
		select {
		case <-ctx.Done():
			return repository.Diff{}, ctx.Err()
		case <-s.secondRelease:
		}
	}
	return s.fakeSession.Diff(ctx, file, options)
}

func runFullscreenBackground(t *testing.T, w, h int) {
	t.Helper()
	session := &fullscreenBackgroundDiffSession{
		fakeSession: fakeSession{
			snapshot: repository.Snapshot{Root: "/work/project", Branch: "main", Files: []repository.File{
				{Path: "a.txt", Status: " M", Unstaged: true, Tracked: true},
				{Path: "b.txt", Status: " M", Unstaged: true, Tracked: true},
			}},
			patch: twoHunkPatch,
		},
		secondStarted: make(chan struct{}, 1),
		secondRelease: make(chan struct{}),
	}
	var once sync.Once
	releaseSecond := func() { once.Do(func() { close(session.secondRelease) }) }
	t.Cleanup(releaseSecond)

	a := newApp(fixedSessionOpener{session: session})
	a.workspaceTab = sectionChanges
	hns := &harness{app: a, updates: newUpdates()}
	hns.tester = ui.NewTester(a.view, w, h)
	a.update = hns.updates.update
	a.path = "/work/project"
	openFixture(t, hns.tester)
	hns.settle(t)
	hns.waitFor(t, "the first file's diff", func() bool {
		return a.diffState != nil && !a.diffLoading
	})
	a.openDiffFullscreen()
	hns.tester.Frame()

	theme := themeForSettings(a.settings, true)
	probe := func(x, y int) color.RGBA {
		return color.RGBAModel.Convert(hns.tester.Image().At(x, y)).(color.RGBA)
	}
	backdrop := color.RGBA{R: theme.Background.R / 2, G: theme.Background.G / 2, B: theme.Background.B / 2, A: 255}
	m := 6
	probes := [][2]int{
		{w - m, h / 3}, {w - m, h / 2}, {w - m, h - m},
		{w * 3 / 4, h / 3}, {w * 3 / 4, h / 2}, {w * 3 / 4, h - m},
		{w * 3 / 5, h / 2}, {w / 2, h / 2}, {w / 2, h - m},
	}

	assertBackground := func(stage string) {
		t.Helper()
		hns.tester.Frame()
		for _, p := range probes {
			if p[0] >= w || p[1] >= h || p[0] < 0 || p[1] < 0 {
				continue
			}
			if got := probe(p[0], p[1]); got == backdrop {
				t.Errorf("%s: backdrop shows through at probe %v (window %dx%d)", stage, p, w, h)
			}
		}
	}
	shot := func(name string) {
		dir := os.Getenv("TANGIT_DEBUG_SHOTS")
		if dir == "" {
			return
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Errorf("create shot dir: %v", err)
			return
		}
		path := filepath.Join(dir, fmt.Sprintf("%s-%dx%d.png", name, w, h))
		f, err := os.Create(path)
		if err != nil {
			t.Errorf("create %s: %v", path, err)
			return
		}
		defer f.Close()
		if err := png.Encode(f, hns.tester.Image()); err != nil {
			t.Errorf("write %s: %v", path, err)
		}
	}

	assertBackground("before navigation")
	shot("before-navigation")

	a.changedFileNavigation(1)
	deadline := time.Now().Add(5 * time.Second)
	if !waitForCond(deadline, func() bool {
		hns.tester.Frame()
		select {
		case <-session.secondStarted:
			return true
		default:
			return false
		}
	}) {
		change, _ := a.selectedChange()
		t.Fatalf("navigating did not start a diff read (file=%q loading=%v key=%q error=%q)", change.file.Path, a.diffLoading, a.diffKey, a.diffError)
	}
	if change, _ := a.selectedChange(); change.file.Path != "b.txt" || !a.diffLoading {
		t.Fatalf("navigation did not leave the second diff loading: file=%q loading=%v", change.file.Path, a.diffLoading)
	}
	shot("while-loading")
	assertBackground("while the next file loads")

	releaseSecond()
	hns.waitFor(t, "the second file's diff", func() bool {
		return a.diffKey == "b.txt\x00unstaged" && a.diffState != nil && !a.diffLoading
	})
	hns.tester.Frame()
	shot("after-load")
	assertBackground("after the next file loads")
}

func waitForCond(deadline time.Time, ok func() bool) bool {
	for time.Now().Before(deadline) {
		if ok() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return ok()
}

// The maximized diff is reported to lose its surfaces (the background shows
// through as a dark void) when the reader moves to the next page of files.
func TestWholeWindowDiffNavigationKeepsBackground(t *testing.T) {
	boxes := [][2]int{{1280, 800}, {1329, 172}, {900, 400}}
	for _, box := range boxes {
		t.Run(fmt.Sprintf("%dx%d", box[0], box[1]), func(t *testing.T) {
			runFullscreenBackground(t, box[0], box[1])
		})
	}
}
