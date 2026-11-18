//go:build !js

package mygo

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/internal/wait"
)

func TestPower(t *testing.T) {
	var got []string
	record := func(event string) func() { return func() { got = append(got, event) } }
	offs := []func(){
		Power.OnSuspend(record("suspend")),
		Power.OnResume(record("resume")),
		Power.OnLockScreen(record("lock")),
		Power.OnUnlockScreen(record("unlock")),
	}
	defer func() {
		for _, off := range offs {
			off()
		}
	}()
	wait.Until(t, "the backend asked to watch power events", func() bool { return onMainValue(func() bool { return fb.Watching }) })
	onMain(func() {
		for _, e := range []string{"suspend", "resume", "lock-screen", "unlock-screen", "unknown"} {
			fb.EmitPower(e)
		}
	})
	if want := "suspend resume lock unlock"; join(got) != want {
		t.Errorf("events = %q, want %q", join(got), want)
	}

	release := Power.KeepAwake("testing", true)
	if n := onMainValue(func() int { return fb.Awake }); n != 1 {
		t.Errorf("awake = %d", n)
	}
	release()
	release() // once
	if n := onMainValue(func() int { return fb.Awake }); n != 0 {
		t.Errorf("awake after release = %d", n)
	}

	release = Power.KeepRunning("serving")
	if n := onMainValue(func() int { return fb.Running }); n != 1 {
		t.Errorf("running = %d", n)
	}
	if n := onMainValue(func() int { return fb.Awake }); n != 0 {
		t.Errorf("KeepRunning kept the computer awake (%d)", n)
	}
	release()
	release() // once
	if n := onMainValue(func() int { return fb.Running }); n != 0 {
		t.Errorf("running after release = %d", n)
	}
	if Power.IsOnBattery() || Power.IdleTime() != 42*time.Second {
		t.Error("IsOnBattery or IdleTime")
	}
}

func join(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += " "
		}
		out += x
	}
	return out
}

func TestDockMenuAndRelaunch(t *testing.T) {
	m := NewMenu([]*MenuItem{{Label: "New Window", ID: "new"}})
	App.Dock.SetMenu(m)
	if App.Dock.Menu() != m {
		t.Error("Dock.Menu")
	}
	dm := onMainValue(func() any { return fb.DockMenu })
	if dm == nil || len(fb.DockMenu.Items) != 1 || fb.DockMenu.Items[0].Label != "New Window" {
		t.Errorf("dock menu = %+v", dm)
	}
	App.Dock.SetMenu(nil)
	if onMainValue(func() bool { return fb.DockMenu != nil }) {
		t.Error("SetMenu(nil) kept the menu")
	}

	// A relaunch whose quit is canceled does nothing. Before the windows
	// close, it lets the app it starts take the foreground, which the
	// app gives up as they close (Windows).
	canceled := make(chan struct{}, 1)
	allowed := onMainValue(func() int { return fb.ForegroundAllowed })
	off := App.OnBeforeQuit(func(e *QuitEvent) {
		if fb.ForegroundAllowed != allowed+1 {
			t.Errorf("the foreground was passed on %d times before the quit", fb.ForegroundAllowed-allowed)
		}
		e.PreventDefault()
		canceled <- struct{}{}
	})
	defer off()
	App.Relaunch("--after-restart")
	select {
	case <-canceled:
	case <-time.After(wait.Guard):
		t.Fatal("Relaunch did not start quitting")
	}
	if onMainValue(func() bool { return App.relaunch || App.relaunchArgs != nil }) {
		t.Error("a canceled relaunch is still pending, or its arguments")
	}
}

// TanGit cancels the window close to confirm edits and flush remote writes.
// Retrying that request must still relaunch after Relaunch clears its flags.
func TestQuitRetryPreservesRelaunchAfterWindowCancellation(t *testing.T) {
	for _, tc := range []struct {
		name     string
		relaunch bool
		args     []string
	}{
		{name: "quit"},
		{name: "relaunch", relaunch: true},
		{name: "relaunch with arguments", relaunch: true, args: []string{"--repo", "a path/with spaces"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _ := testWindow(t, WindowOptions{})
			var retry func()
			attempts := 0
			off := App.OnBeforeQuit(func(e *QuitEvent) {
				attempts++
				if attempts == 1 {
					retry = e.Retry
					return
				}
				// Inspect the resumed request without actually ending the test app.
				e.PreventDefault()
				if App.relaunch != tc.relaunch || !slices.Equal(App.relaunchArgs, tc.args) {
					t.Errorf("resumed request = relaunch %v args %q, want %v %q", App.relaunch, App.relaunchArgs, tc.relaunch, tc.args)
				}
			})
			defer off()
			w.OnClose(func(e *CloseEvent) { e.PreventDefault() })
			if tc.relaunch {
				App.Relaunch(tc.args...)
			} else {
				App.Quit()
			}
			onMain(func() {
				if attempts != 1 || retry == nil || w.IsDestroyed() {
					t.Error("window cancellation did not preserve the pending confirmation")
					return
				}
				if App.relaunch || App.relaunchArgs != nil {
					t.Error("canceled relaunch remained pending")
					return
				}
				// This represents the asynchronous confirmation/save completing.
				retry()
			})
			onMain(func() {
				if attempts != 2 {
					t.Errorf("quit attempts = %d, want 2", attempts)
				}
			})
		})
	}
}

func TestQuitRetryDoesNothingWhenSessionEnding(t *testing.T) {
	called := false
	e := &QuitEvent{SessionEnding: true, retry: func() { called = true }}
	e.Retry()
	if called {
		t.Fatal("session-ending quit was retried")
	}
}

func TestPermissionHandler(t *testing.T) {
	w, fw := testWindow(t, WindowOptions{})
	ask := func(kinds []string, origin string) bool {
		return onMainValue(func() bool { return fw.H.PermissionRequested(kinds, origin) })
	}
	// By default the app's own pages get what they ask for.
	if !ask([]string{"camera"}, "mygo://localhost") || ask([]string{"camera"}, "https://evil.example") {
		t.Error("default permission decisions")
	}
	var got PermissionRequest
	w.Page().SetPermissionHandler(func(req PermissionRequest) bool {
		got = req
		return req.Origin == "https://meet.example"
	})
	if !ask([]string{"camera", "microphone"}, "https://meet.example") || ask([]string{"geolocation"}, "mygo://localhost") {
		t.Error("the handler's decisions were not applied")
	}
	if got.Origin != "mygo://localhost" || len(got.Permissions) != 1 || got.Permissions[0] != PermissionGeolocation {
		t.Errorf("last request = %+v", got)
	}
	w.Page().SetPermissionHandler(nil)
	if !ask([]string{"camera"}, "mygo://localhost") {
		t.Error("SetPermissionHandler(nil) did not restore the default")
	}
}

func TestClearBrowsingDataFake(t *testing.T) {
	before := onMainValue(func() int { return fb.Cleared })
	if err := App.ClearBrowsingData(); err != nil {
		t.Fatal(err)
	}
	if onMainValue(func() int { return fb.Cleared }) != before+1 {
		t.Error("the backend did not clear")
	}
}
