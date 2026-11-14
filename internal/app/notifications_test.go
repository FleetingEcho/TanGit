package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// failingSession is the fixture's session with one action failing, so a test
// can see what a failed Git command reports.
type failingSession struct{ fakeSession }

func (s failingSession) Push(context.Context) error {
	return errors.New("! [rejected] main -> main (non-fast-forward)")
}

func TestCopyAnnouncesWhatWasCopied(t *testing.T) {
	h := menuTestApp(t)
	h.press(t, sectionKeys[sectionBranches])

	h.rightClick(t, "feature")
	h.chooseMenuItem(t, "Copy branch name")

	texts := h.texts()
	if !slices.Contains(texts, "Copied branch name") {
		t.Fatalf("the copy showed no notification: %q", texts)
	}
	if !slices.Contains(texts, "feature") {
		t.Fatalf("the notification does not name the value copied: %q", texts)
	}
}

func stubScopedTranslationCatalog(t *testing.T, entries map[string]string) {
	t.Helper()
	const language = "scoped-test"
	previousLanguage := currentLanguage
	previousCatalog, previouslyRegistered := translations[language]
	translations[language] = entries
	currentLanguage = language
	t.Cleanup(func() {
		currentLanguage = previousLanguage
		if previouslyRegistered {
			translations[language] = previousCatalog
		} else {
			delete(translations, language)
		}
	})
}

func TestNotificationDescriptionTranslationIsExplicit(t *testing.T) {
	const staticDescription = "A static app-authored description"
	const serverDescription = "server payload that resembles a catalog key"
	stubScopedTranslationCatalog(t, map[string]string{
		staticDescription: "translated static description",
		serverDescription: "must not be used",
	})
	a := &app{}
	a.notifyTranslated("info", "Notice", staticDescription)
	a.notify("error", "Notice", serverDescription)
	if got := a.pendingToasts[0].description; got != "translated static description" {
		t.Fatalf("static description = %q", got)
	}
	if got := a.pendingToasts[1].description; got != serverDescription {
		t.Fatalf("dynamic description passed through translation: %q", got)
	}
}

func TestErrorsAndToastsRedactCredentials(t *testing.T) {
	a := &app{}
	err := errors.New("fetch https://alice:super-secret@example.com/private.git failed: Authorization: Bearer toast-secret")
	a.actionError = safeError(err)
	a.notifyError(err)

	if len(a.pendingToasts) != 1 {
		t.Fatalf("%d error toasts, want 1", len(a.pendingToasts))
	}
	presented := a.actionError + "\n" + a.pendingToasts[0].description
	for _, secret := range []string{"alice", "super-secret", "toast-secret"} {
		if strings.Contains(presented, secret) {
			t.Fatalf("presented error contains %q: %q", secret, presented)
		}
	}
	if !strings.Contains(presented, "[redacted]") || !strings.Contains(presented, "example.com/private.git") {
		t.Fatalf("redaction removed the useful failure context: %q", presented)
	}
}

func TestFailedActionOffersTheCommandLog(t *testing.T) {
	session := failingSession{fakeSession{
		snapshot: repository.Snapshot{
			Root:   "/work/project",
			Branch: "main",
			Branches: []repository.Branch{
				{Name: "main", Head: true},
			},
		},
		commandLog: repository.NewCommandLog(),
	}}
	a := newApp(fixedSessionOpener{session: session})
	a.workspaceTab = sectionChanges
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)

	if err := h.tester.Click("Push"); err != nil {
		t.Fatal(err)
	}
	h.confirmAttentionForTest(t)
	h.waitFor(t, "the push to fail", func() bool {
		return slices.Contains(h.texts(), "Git action failed")
	})
	if !slices.Contains(h.texts(), "View log") {
		t.Fatalf("the error notification offers no way to the log: %q", h.texts())
	}

	// The command log holds the failure, which is what the notification
	// promises.
	entries := a.commandLogEntries()
	if len(entries) == 0 || !entries[len(entries)-1].Failed {
		t.Fatalf("the command log records no failure: %+v", entries)
	}
	if got := formatCommandLog(entries); !strings.Contains(got, "non-fast-forward") {
		t.Fatalf("the command log does not hold Git's message: %q", got)
	}
}

type rejectedPushSession struct{ fakeSession }

func (s rejectedPushSession) Push(context.Context) error {
	return fmt.Errorf("push: %w: ! [rejected] main -> main (non-fast-forward)", repository.ErrPushRejected)
}

func (s rejectedPushSession) Pull(context.Context) error {
	s.calls.record("pull")
	return nil
}

func TestRejectedPushOffersPullBeforeRetry(t *testing.T) {
	calls := &callRecorder{}
	session := rejectedPushSession{fakeSession{
		snapshot: repository.Snapshot{
			Root:   "/work/project",
			Branch: "main",
			Branches: []repository.Branch{
				{Name: "main", Head: true, UpstreamRemote: "origin", UpstreamBranch: "main"},
			},
		},
		calls:      calls,
		commandLog: repository.NewCommandLog(),
	}}
	a := newApp(fixedSessionOpener{session: session})
	a.workspaceTab = sectionChanges
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)

	if err := h.tester.Click("Push"); err != nil {
		t.Fatal(err)
	}
	h.confirmAttentionForTest(t)
	h.waitFor(t, "the pull-first notification", func() bool {
		return slices.Contains(h.texts(), "Pull first, then push")
	})
	for _, want := range []string{
		"The remote branch has changes you do not have locally. Pull and resolve any conflicts, then push again.",
		"Pull now",
		"View log",
	} {
		if !slices.Contains(h.texts(), want) {
			t.Errorf("rejected-push notification is missing %q: %q", want, h.texts())
		}
	}
	if calls.has("pull") {
		t.Fatal("the rejected push pulled without the user's approval")
	}
	entries := a.commandLogEntries()
	if got := formatCommandLog(entries); !strings.Contains(got, "non-fast-forward") {
		t.Fatalf("the command log lost Git's rejection details: %q", got)
	}

	if err := h.tester.Click("Pull now"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	if a.confirm == nil || a.confirm.title != "Pull" {
		t.Fatalf("Pull now did not open the normal Pull confirmation: %+v", a.confirm)
	}
	h.confirmAttentionForTest(t)
	h.waitFor(t, "the pull", func() bool { return calls.has("pull") })
}

type divergedPullSession struct{ fakeSession }

func (s divergedPullSession) Pull(context.Context) error {
	return fmt.Errorf("pull: %w: fatal: Need to specify how to reconcile divergent branches", repository.ErrPullDiverged)
}

func (s divergedPullSession) PullWithStrategy(_ context.Context, strategy repository.PullStrategy) error {
	s.calls.record(fmt.Sprintf("pull-strategy:%d", strategy))
	return nil
}

func TestDivergedPullOffersMergeAndRebaseRecovery(t *testing.T) {
	for _, test := range []struct {
		name     string
		action   string
		title    string
		strategy repository.PullStrategy
	}{
		{name: "merge", action: "Merge", title: "Pull with merge", strategy: repository.PullMerge},
		{name: "rebase", action: "Rebase", title: "Pull with rebase", strategy: repository.PullRebase},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := &callRecorder{}
			session := divergedPullSession{fakeSession{
				snapshot: repository.Snapshot{
					Root:   "/work/project",
					Branch: "main",
					Branches: []repository.Branch{
						{Name: "main", Head: true, UpstreamRemote: "origin", UpstreamBranch: "main"},
					},
				},
				calls:      calls,
				commandLog: repository.NewCommandLog(),
			}}
			a := newApp(fixedSessionOpener{session: session})
			a.workspaceTab = sectionChanges
			h := &harness{app: a, updates: newUpdates()}
			h.tester = ui.NewTester(a.view, 1280, 800)
			a.update = h.updates.update
			a.path = "/work/project"
			openFixture(t, h.tester)
			h.settle(t)

			if err := h.tester.Click("Pull"); err != nil {
				t.Fatal(err)
			}
			h.confirmAttentionForTest(t)
			h.waitFor(t, "the divergence recovery notification", func() bool {
				return slices.Contains(h.texts(), "Choose how to pull")
			})
			for _, want := range []string{
				"Your local and remote branches have diverged. Merge preserves both histories; rebase replays your local commits on top of the remote tip.",
				"Merge",
				"Rebase",
				"View log",
			} {
				if !slices.Contains(h.texts(), want) {
					t.Errorf("divergence notification is missing %q: %q", want, h.texts())
				}
			}
			if len(calls.all()) != 0 {
				t.Fatalf("divergence recovery ran before a strategy was chosen: %v", calls.all())
			}

			if err := h.tester.Click(test.action); err != nil {
				t.Fatal(err)
			}
			h.tester.Frame()
			if a.confirm == nil || a.confirm.title != test.title {
				t.Fatalf("%s did not open its confirmation: %+v", test.action, a.confirm)
			}
			h.confirmAttentionForTest(t)
			wantCall := fmt.Sprintf("pull-strategy:%d", test.strategy)
			h.waitFor(t, "the strategy-specific pull", func() bool { return calls.has(wantCall) })
		})
	}
}

func TestCommandLogModalShowsAndClearsCommands(t *testing.T) {
	log := repository.NewCommandLog()
	log.Note("fatal: not a git repository")
	a := newApp(fakeOpener{result: openResult{snapshot: repository.Snapshot{
		Root:     "/work/project",
		Branch:   "main",
		Branches: []repository.Branch{{Name: "main", Head: true}},
	}}})
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	// The fake session has no log of its own; the open keeps what the
	// session gave, so the fixture's is put in its place.
	a.commandLog = log

	// The status bar's control opens the log.
	if err := h.tester.Click("Command log"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.settle(t)
	texts := h.texts()
	if !slices.Contains(texts, "$ Git action") {
		t.Fatalf("the command log lists no command: %q", texts)
	}
	if !strings.Contains(strings.Join(texts, "\n"), "not a git repository") {
		t.Fatalf("the command log hides the output: %q", texts)
	}

	// Escape closes it, and Ctrl+L opens it again.
	h.press(t, ui.KeyEscape)
	if a.commandLogOpen {
		t.Fatal("Escape did not close the command log")
	}
	h.tester.Key(ui.Ctrl, ui.KeyL)
	h.tester.Frame()
	h.settle(t)
	if !a.commandLogOpen {
		t.Fatal("Ctrl+L did not open the command log")
	}

	// Clear drops the commands, and the pane says so.
	if err := h.tester.Click("Clear"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.settle(t)
	if entries := log.Entries(); len(entries) != 0 {
		t.Fatalf("Clear left %d commands", len(entries))
	}
	if !slices.Contains(h.texts(), "No Git commands yet") {
		t.Fatalf("the cleared log shows no empty state: %q", h.texts())
	}
}

// A notification shows at the window's top right, under the tab bar and its
// divider, where a new one is noticed: not at the bottom over the work, and
// never over the window's own controls. The newest sits highest, so it
// always appears in the same place.
func TestNotificationsShowAtTheTopRight(t *testing.T) {
	s, tt, _ := newShellTester(t, repository.Snapshot{})
	s.activeApp().notify("success", "Copied commit hash", "9f2c1ab4e7d8c3f0")
	s.activeApp().notify("error", "Git action failed", "fatal: could not read")
	tt.Frame()
	width, height := tt.Image().Bounds().Dx(), tt.Image().Bounds().Dy()
	newest, ok := tt.Find("Git action failed")
	if !ok {
		t.Fatalf("the notification did not show: %v", tt.Texts())
	}
	if newest.Y < titleBarHeight+1 {
		t.Fatalf("the notification covers the window's chrome at y=%v", newest.Y)
	}
	if newest.Y > float32(height)/2 {
		t.Fatalf("the notification sits low, at y=%v", newest.Y)
	}
	if newest.X < float32(width)/2 {
		t.Fatalf("the notification is not at the right edge: x=%v", newest.X)
	}
	older, ok := tt.Find("Copied commit hash")
	if !ok {
		t.Fatalf("the second notification did not show: %v", tt.Texts())
	}
	if newest.Y > older.Y {
		t.Fatalf("the newest notification is below the older one: %v then %v", newest.Y, older.Y)
	}
}

// The log is text to keep, not a picture of it: a command or a line of
// output can be selected and copied, so Git's message can go into a ticket
// or an email without copying the whole record.
func TestCommandLogTextCanBeCopied(t *testing.T) {
	log := repository.NewCommandLog()
	log.Note("fatal: not a git repository")
	a := newApp(fakeOpener{result: openResult{snapshot: repository.Snapshot{
		Root:     "/work/project",
		Branch:   "main",
		Branches: []repository.Branch{{Name: "main", Head: true}},
	}}})
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	a.commandLog = log

	if err := h.tester.Click("Command log"); err != nil {
		t.Fatal(err)
	}
	h.tester.Frame()
	h.settle(t)

	line, ok := h.tester.Find("fatal: not a git repository")
	if !ok {
		t.Fatalf("the log does not show the output: %q", h.texts())
	}
	// A click in the text puts the selection there; the keyboard then
	// takes the whole log, and copies what it reads.
	h.tester.ClickAt(line.X+4, line.Y+line.H/2)
	h.tester.Frame()
	h.tester.Key(ui.Ctrl, ui.KeyA)
	h.tester.Frame()
	h.tester.Key(ui.Ctrl, ui.KeyC)
	h.tester.Frame()
	if got := h.tester.Clipboard(); !strings.Contains(got, "fatal: not a git repository") {
		t.Fatalf("copying the log put %q on the clipboard", got)
	}
}

func TestRichNotificationReplacesProgressAndRunsSecondaryAction(t *testing.T) {
	s, tt, _ := newShellTester(t, repository.Snapshot{})
	a := s.activeApp()
	a.notifyToast(appToast{
		id: "workspace-transfer:test", title: "Uploading", description: "1 of 3 files", timeout: -1,
	})
	tt.Frame()
	revealed := false
	a.notifyToast(appToast{
		id: "workspace-transfer:test", kind: "success", title: "Upload complete", description: "3 files",
		timeout: 4 * time.Second, moreActions: []appToastAction{{label: "Reveal", onAction: func() { revealed = true }}},
	})
	tt.Frame()
	if slices.Contains(tt.Texts(), "Uploading") || !slices.Contains(tt.Texts(), "Upload complete") {
		t.Fatalf("progress notification was not replaced: %q", tt.Texts())
	}
	if count := strings.Count(strings.Join(tt.Texts(), "\n"), "3 files"); count != 1 {
		t.Fatalf("updated notification appears %d times, want once", count)
	}
	if err := tt.Click("Reveal"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !revealed {
		t.Fatal("secondary notification action did not run")
	}
}

func TestBranchMenuSyncsAndForcesCheckout(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.press(t, sectionKeys[sectionBranches])

	// The checked-out branch pulls its upstream.
	h.rightClick(t, "main (local)")
	h.requireMenuLabels(t, "Pull latest", "Fast-forward")
	h.chooseMenuItem(t, "Fast-forward")
	h.confirmAttentionForTest(t)
	h.waitFor(t, "the fast-forward", func() bool { return calls.has("fast-forward:main") })

	// Another branch can be checked out over the work in the way, once the
	// confirmation says what it discards.
	h.rightClick(t, "feature")
	h.chooseMenuItem(t, "Force checkout…")
	if h.app.confirm == nil {
		t.Fatal("Force checkout asked no confirmation")
	}
	h.tester.Click("Force checkout")
	h.waitFor(t, "the forced checkout", func() bool { return calls.has("force-checkout:feature") })

	// Rebase asks too, because it rewrites the current branch's commits.
	h.app.confirm = nil
	h.rightClick(t, "feature")
	h.chooseMenuItem(t, "Rebase current onto this…")
	if h.app.confirm == nil {
		t.Fatal("Rebase asked no confirmation")
	}
	h.tester.Click("Rebase")
	h.waitFor(t, "the rebase", func() bool { return calls.has("rebase-onto:feature") })
}

func TestConfirmCancelRunsNothing(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.selectedFile = 1

	h.rightClick(t, "two.txt")
	h.chooseMenuItem(t, "Discard changes…")
	if h.app.confirm == nil {
		t.Fatal("Discard asked no confirmation")
	}
	h.press(t, ui.KeyEscape)
	if h.app.confirm != nil {
		t.Fatal("Escape left the confirmation open")
	}
	if calls.has("discard:two.txt") {
		t.Fatal("cancelling the confirmation still discarded the file")
	}

	// Answering it runs the action.
	h.rightClick(t, "two.txt")
	h.chooseMenuItem(t, "Discard changes…")
	if err := h.tester.Click("Discard"); err != nil {
		t.Fatal(err)
	}
	h.waitFor(t, "the discard", func() bool { return calls.has("discard:two.txt") })
	if h.app.confirm != nil {
		t.Fatal("the confirmation stayed open after it ran")
	}
}

// verboseFailingSession fails a push the way a fetch can: with pages of
// output and Git's fatal line buried at the end.
type verboseFailingSession struct{ fakeSession }

func (s verboseFailingSession) Push(context.Context) error {
	return errors.New("fetch: +--------------------------------------------------+\n" +
		"       | REMOTE REPOSITORY ACCESS NOTICE                 |\n" +
		"       +--------------------------------------------------+\n" +
		"       | Source code stored on this server is private.     |\n" +
		"       | Access is limited to authorized contributors.     |\n" +
		"       +--------------------------------------------------+\n" +
		"remote: ==========================================\n" +
		"remote: OTP verification is required to access the repository.\n" +
		"remote: Use: ssh git@git.example.com verify_device\n" +
		"remote: ==========================================\n" +
		"fatal: Could not read from remote repository.\n" +
		"\n" +
		"Please make sure you have the correct access rights\n" +
		"and the repository exists.")
}

// A Git error can run to pages, but the status bar shows one line of it: the
// whole message made the bar wrap, and its height squeezed the workspace off
// the top of the window.
func TestLongActionErrorStaysOnOneStatusLine(t *testing.T) {
	session := verboseFailingSession{fakeSession{
		snapshot: repository.Snapshot{
			Root:     "/work/project",
			Branch:   "main",
			Branches: []repository.Branch{{Name: "main", Head: true}},
		},
		commandLog: repository.NewCommandLog(),
	}}
	a := newApp(fixedSessionOpener{session: session})
	a.workspaceTab = sectionChanges
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)

	if err := h.tester.Click("Push"); err != nil {
		t.Fatal(err)
	}
	h.confirmAttentionForTest(t)
	h.waitFor(t, "the push to fail", func() bool {
		return slices.Contains(h.texts(), "Git action failed")
	})
	h.settle(t)

	line, ok := h.tester.Find("fatal: Could not read from remote repository.")
	if !ok {
		t.Fatalf("the status bar does not show Git's fatal line: %q", h.texts())
	}
	if line.H > 40 {
		t.Fatalf("the status bar wrapped the error into %v px", line.H)
	}
	if line.Y < 700 {
		t.Fatalf("the error pushed the status bar up to y=%v", line.Y)
	}
	// The workspace keeps its room: the Changes panel's commit box still
	// sits near the top rather than being squeezed out of the window.
	if box, ok := h.tester.Find("Commit message"); !ok || box.Y > 300 {
		t.Fatalf("the workspace lost its room: the commit box is at y=%v (%v)", box.Y, ok)
	}
}

// The window's chrome shows no usage hints: the sidebar's sections and the
// rows speak for themselves, and the status bar keeps only what acts, the
// command log button.
func TestChromeShowsNoUsageHints(t *testing.T) {
	h := menuTestApp(t)
	for _, hint := range []string{"1–9 and 0 switch section", "Right-click rows for actions"} {
		if containsText(h.texts(), hint) {
			t.Errorf("the chrome still shows the hint %q", hint)
		}
	}
	if !containsText(h.texts(), "Command log") {
		t.Fatalf("the status bar lost its command log button: %q", h.texts())
	}
}
