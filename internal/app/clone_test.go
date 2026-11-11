package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

type cloneAttempt struct {
	options  repository.CloneOptions
	progress []repository.CloneProgress
}

type cloneTestOpener struct {
	fakeOpener
	started  chan cloneAttempt
	release  chan error
	cloneErr error
}

func (o *cloneTestOpener) Clone(ctx context.Context, options repository.CloneOptions, report func(repository.CloneProgress)) error {
	attempt := cloneAttempt{options: options, progress: []repository.CloneProgress{{
		Phase: "Receiving objects", Current: 3, Total: 12, Percent: 25, Fraction: 0.25, Known: true,
		Detail: "Receiving objects: 25% (3/12)",
	}}}
	o.started <- attempt
	for _, progress := range attempt.progress {
		report(progress)
	}
	if o.release != nil {
		select {
		case err := <-o.release:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return o.cloneErr
}

func cloneTestApp(t *testing.T, opener *cloneTestOpener) *harness {
	t.Helper()
	a := newApp(opener)
	h := &harness{app: a, updates: newUpdates(), tester: ui.NewTester(a.view, 1280, 800)}
	a.update = h.updates.update
	h.tester.Frame()
	return h
}

func cloneOptionsFixture() (repository.CloneOptions, string) {
	destination := filepath.Join(string(filepath.Separator), "tmp", "clone-tests", "project")
	return repository.CloneOptions{
		URL: "https://example.com/owner/project.git", Destination: destination, Branch: "topic",
	}, destination
}

func TestCloneFormSuccessUsesExistingOpenPipeline(t *testing.T) {
	options, destination := cloneOptionsFixture()
	opener := &cloneTestOpener{
		fakeOpener: fakeOpener{result: openResult{snapshot: repository.Snapshot{Root: destination, Branch: "topic"}}},
		started:    make(chan cloneAttempt, 1), release: make(chan error, 1),
	}
	h := cloneTestApp(t, opener)
	h.tester.Click("Clone")
	h.tester.Frame()
	if !h.app.cloneOpen {
		t.Fatal("Home entry did not open the clone form")
	}
	h.app.cloneURL = options.URL
	h.app.cloneDestination = options.Destination
	h.app.cloneBranch = options.Branch
	if err := h.tester.Click("Clone and open"); err != nil {
		t.Fatal(err)
	}
	attempt := <-opener.started
	if attempt.options != options {
		t.Fatalf("clone options = %+v, want %+v", attempt.options, options)
	}
	h.waitFor(t, "real parsed progress", func() bool { return h.app.cloneProgress.Fraction == 0.25 })
	if h.app.state != stateCloning {
		t.Fatalf("state = %v, want cloning", h.app.state)
	}
	if !h.tester.HasText("Receiving objects") || !h.tester.HasText("25%") ||
		!h.tester.HasText("Receiving objects · 3/12") {
		t.Fatalf("parsed progress is not visible: %q", h.tester.Texts())
	}
	opener.release <- nil
	h.settle(t)
	if h.app.state != stateReady || h.app.path != destination || h.app.snapshot.Root != destination {
		t.Fatalf("successful clone did not use ordinary open: state=%v path=%q root=%q err=%q",
			h.app.state, h.app.path, h.app.snapshot.Root, h.app.errMessage)
	}
	if h.app.cloneOpen || h.app.cancelClone != nil {
		t.Fatal("successful clone left its form or cancellation active")
	}
}

func TestCloneFailureReturnsEditableSanitizedForm(t *testing.T) {
	options, _ := cloneOptionsFixture()
	opener := &cloneTestOpener{started: make(chan cloneAttempt, 1), cloneErr: errors.New(
		"fatal: https://user:secret@example.com/private.git: authentication failed",
	)}
	h := cloneTestApp(t, opener)
	h.app.openCloneForm()
	h.app.cloneURL = "https://user:secret@example.com/private.git"
	h.app.cloneDestination = options.Destination
	// Production validation rejects this before Git. Invoke a syntactically
	// safe remote so this test can exercise failed Git diagnostics instead.
	h.app.cloneURL = options.URL
	h.app.beginClone()
	<-opener.started
	h.waitFor(t, "the failed clone form", func() bool { return h.app.cloneOpen })
	h.tester.Frame()
	if h.app.state != stateEmpty || h.app.cloneError == "" {
		t.Fatalf("failure state = %v error=%q", h.app.state, h.app.cloneError)
	}
	if !h.tester.HasText("Could not clone repository") || !strings.Contains(strings.Join(h.tester.Texts(), "\n"), "authentication failed") {
		t.Fatalf("failure diagnostic is not visible: %q", h.tester.Texts())
	}
	h.app.cloneURL = "https://user:secret@example.com/private.git"
	h.app.beginClone()
	if !strings.Contains(h.app.cloneURLError, "credentials") || strings.Contains(h.app.cloneURLError, "secret") {
		t.Fatalf("credential validation was not sanitized: %q", h.app.cloneURLError)
	}
	if h.app.cloneError != "" {
		t.Fatalf("field validation also produced a duplicate form error: %q", h.app.cloneError)
	}
	if err := h.tester.Click("Remote repository URL"); err != nil {
		t.Fatal(err)
	}
	h.tester.Key(0, ui.KeyC)
	h.tester.Frame()
	if !h.app.cloneOpen {
		t.Fatal("C inside the clone URL input closed or replaced the form")
	}
}

func TestCloneRequiredFieldsAndInlineValidation(t *testing.T) {
	opener := &cloneTestOpener{started: make(chan cloneAttempt, 1)}
	h := cloneTestApp(t, opener)
	h.app.openCloneForm()
	h.tester.Frame()
	if !h.tester.HasText("Remote repository URL *") || !h.tester.HasText("Destination folder *") {
		t.Fatalf("required fields are not identified: %q", h.tester.Texts())
	}
	if h.tester.HasText("Remote repository URL is required.") {
		t.Fatal("an untouched form already shows validation errors")
	}

	h.app.beginClone()
	h.tester.Frame()
	for _, want := range []string{"Remote repository URL is required.", "Destination folder is required."} {
		if !h.tester.HasText(want) {
			t.Errorf("required-field error %q is not visible: %q", want, h.tester.Texts())
		}
	}
	for _, label := range []string{"Remote repository URL", "Destination folder"} {
		info, ok := h.tester.Access(label)
		if !ok || !info.Invalid || !strings.Contains(info.Description, "required") {
			t.Errorf("%s does not expose its invalid state and error: %+v, %v", label, info, ok)
		}
	}
	if info, _ := h.tester.Access("Branch (optional)"); info.Invalid {
		t.Fatal("empty optional branch was marked invalid")
	}

	h.app.cloneURL = "https://user:secret@example.com/private.git"
	h.app.cloneDestination = "/tmp/bad\npath"
	h.app.cloneBranch = "bad\nbranch"
	h.app.beginClone()
	h.tester.Frame()
	if !strings.Contains(h.app.cloneURLError, "credentials") || strings.Contains(h.app.cloneURLError, "secret") {
		t.Fatalf("URL validation is missing or unsafe: %q", h.app.cloneURLError)
	}
	if h.app.cloneDestinationError == "" || h.app.cloneBranchError == "" {
		t.Fatalf("invalid destination/branch lack field errors: %q / %q", h.app.cloneDestinationError, h.app.cloneBranchError)
	}
	select {
	case <-opener.started:
		t.Fatal("Git clone started despite invalid fields")
	default:
	}

	h.app.cloneURL = "https://example.com/owner/project.git"
	h.app.cloneDestination = filepath.Join(t.TempDir(), "project")
	h.app.cloneBranch = "topic"
	h.app.cloneFieldChanged()
	h.tester.Frame()
	if h.app.cloneURLError != "" || h.app.cloneDestinationError != "" || h.app.cloneBranchError != "" {
		t.Fatalf("corrected fields retained errors: %q / %q / %q", h.app.cloneURLError, h.app.cloneDestinationError, h.app.cloneBranchError)
	}
}

func TestCloneCancelRejectsLateResultAndDoesNotOpen(t *testing.T) {
	options, _ := cloneOptionsFixture()
	opener := &cloneTestOpener{
		started: make(chan cloneAttempt, 1), release: make(chan error, 1),
		fakeOpener: fakeOpener{result: openResult{snapshot: repository.Snapshot{Root: options.Destination}}},
	}
	h := cloneTestApp(t, opener)
	h.app.openCloneForm()
	h.app.cloneURL, h.app.cloneDestination = options.URL, options.Destination
	h.app.beginClone()
	<-opener.started
	h.waitFor(t, "the in-flight clone", func() bool { return h.app.cloneProgress.Known })
	h.tester.Key(0, ui.KeyEscape)
	h.tester.Frame()
	if h.app.state != stateEmpty || h.app.cloneOpen || h.app.cancelClone != nil {
		t.Fatalf("Escape did not cancel: state=%v open=%v", h.app.state, h.app.cloneOpen)
	}
	// The fake can also finish like a process that loses a cancellation race.
	// Its completion must not reopen Home or replace the current destination.
	opener.release <- nil
	for len(h.updates.done) > 0 {
		h.updates.waitForUpdate(t)
		h.tester.Frame()
	}
	h.tester.Frame()
	if h.app.state != stateEmpty || h.app.path != "" || h.app.cloneProgress.Known {
		t.Fatalf("late clone result installed state: state=%v path=%q progress=%+v",
			h.app.state, h.app.path, h.app.cloneProgress)
	}
}

func TestCloneNativeParentChoiceDerivesNameAndRejectsStaleChoice(t *testing.T) {
	a := newApp(fakeOpener{})
	updates := newUpdates()
	a.update = updates.update
	a.cloneParent = filepath.Join(string(filepath.Separator), "tmp", "old")
	a.openCloneForm()
	a.cloneURL = "git@example.com:owner/project.git"
	started := make(chan mygo.OpenDialogOptions, 1)
	release := make(chan []string, 1)
	restore := chooseCloneParent
	chooseCloneParent = func(options mygo.OpenDialogOptions) ([]string, error) {
		started <- options
		return <-release, nil
	}
	t.Cleanup(func() { chooseCloneParent = restore })
	a.browseCloneParent()
	options := <-started
	if !options.Directory || !options.CreateDirectories || options.Title != "Choose clone parent folder" {
		t.Fatalf("native parent options = %+v", options)
	}
	a.closeCloneForm()
	parent := filepath.Join(string(filepath.Separator), "tmp", "new-parent")
	release <- []string{parent}
	updates.waitForUpdate(t)
	if a.cloneDestination != "" || a.cloneOpen {
		t.Fatalf("stale parent choice reopened or edited the form: %q", a.cloneDestination)
	}

	// A live choice uses the selected parent and the remote-derived name.
	a.openCloneForm()
	a.cloneURL = "git@example.com:owner/project.git"
	a.browseCloneParent()
	<-started
	release <- []string{parent}
	updates.waitForUpdate(t)
	if got, want := a.cloneDestination, filepath.Join(parent, "project"); got != want {
		t.Fatalf("destination = %q, want %q", got, want)
	}
}

func TestCloneHotkeyOnlyOnHomeOutsideTextInputsAndHelp(t *testing.T) {
	a := newApp(fakeOpener{})
	tt := ui.NewTester(a.view, 1280, 800)
	tt.Frame()
	// The repository path starts focused, so an ordinary C belongs to it.
	tt.Key(0, ui.KeyC)
	tt.Frame()
	if a.cloneOpen {
		t.Fatal("C opened clone form while repository path accepted text")
	}
	tt.ClickAt(10, 790) // focus no text control, still Home
	tt.Key(0, ui.KeyC)
	tt.Frame()
	if !a.cloneOpen {
		t.Fatal("C did not open the clone form on Home outside text input")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.cloneOpen {
		t.Fatal("Escape did not close the clone form")
	}
	tt.Key(ui.Shift, ui.KeySlash)
	tt.Frame()
	text := strings.Join(tt.Texts(), " ")
	for _, want := range []string{"Home", "C", "Clone a remote repository", "Cancel cloning"} {
		if !strings.Contains(text, want) {
			t.Errorf("Home help is missing %q: %v", want, tt.Texts())
		}
	}
}

func TestCloneFormAndProgressIntegrateWithFormLifecycle(t *testing.T) {
	a := newApp(fakeOpener{})
	a.openCloneForm()
	if !a.anyFormOpen() || !a.modalOpen() {
		t.Fatal("clone form is not part of modal keyboard ownership")
	}
	a.closeAllForms()
	if a.cloneOpen || a.anyFormOpen() {
		t.Fatal("closeAllForms left the clone form open")
	}
	a.openCloneForm()
	a.cloneURL = "https://example.com/owner/project.git"
	a.cloneDestination = filepath.Join(t.TempDir(), "project")
	a.state = stateCloning
	ctx, cancel := context.WithCancel(context.Background())
	a.cancelClone = cancel
	a.cancelCloneOperation()
	if a.state != stateEmpty || a.modalOpen() {
		t.Fatal("canceling clone left modal state")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("canceling clone did not cancel its Git context")
	}
}
