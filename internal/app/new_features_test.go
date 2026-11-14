package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/egoist/mygo/ui/editor"

	"lazygitapp/internal/repository"
)

func TestCustomCommandKeyParsingRoundTrip(t *testing.T) {
	for _, input := range []string{"ctrl+g", "shift+f2", "alt+left", "super+comma", "f9", "space"} {
		mods, key, ok := parseCustomCommandKey(input)
		if !ok {
			t.Fatalf("parseCustomCommandKey(%q) rejected a valid key", input)
		}
		if mods != 0 && mods&(ui.Ctrl|ui.Alt|ui.Shift|ui.Super) == 0 {
			t.Fatalf("invalid modifier mask for %q", input)
		}
		if key == 0 {
			t.Fatalf("invalid key for %q", input)
		}
		if got := customCommandDisplayKey(input); got == "" {
			t.Fatalf("empty display for %q", input)
		}
	}
	for _, input := range []string{"ctrl+", "hyper+a", "f13", "unknown", "notakey"} {
		if _, _, ok := parseCustomCommandKey(input); ok {
			t.Fatalf("parseCustomCommandKey(%q) accepted an invalid key", input)
		}
	}
}

func TestNormalizeCustomCommandsFiltersAndScopes(t *testing.T) {
	commands := normalizeCustomCommands([]customCommandSetting{
		{Name: "", Command: "make build", Key: "F5", Contexts: []string{"history", "graph"}},
		{Name: "ignored", Command: " ", Key: "ctrl+x"},
		{Name: "publish", Command: "make publish", Key: "not a key", Contexts: []string{"everywhere", "history", "changes"}},
	})
	if len(commands) != 2 {
		t.Fatalf("got %d commands, want 2", len(commands))
	}
	if commands[0].Name != "make build" || commands[0].Key != "f5" {
		t.Fatalf("first command = %+v", commands[0])
	}
	if commands[1].Key != "" || len(commands[1].Contexts) != 1 || commands[1].Contexts[0] != "everywhere" {
		t.Fatalf("invalid normalization: %+v", commands[1])
	}
	if !customCommandInContext(commands[0], "History and graph") {
		t.Fatal("history context must cover the History and graph command group")
	}
	if customCommandInContext(commands[0], "Branches") {
		t.Fatal("history-only command leaked into Branches")
	}
	if !customCommandInContext(commands[1], "Changes") {
		t.Fatal("everywhere command must cover Changes")
	}
}

func TestCustomCommandPlaceholdersAreQuoted(t *testing.T) {
	values := map[string]string{"branch": "topic with spaces", "file": "a'b;rm -rf /"}
	got := customCommandPlaceholders("echo {{branch}} {{file}}", values)
	if !strings.ContainsRune(got, '\'') {
		t.Fatalf("unquoted shell value: %q", got)
	}
	if !containsShellWord(got, "topic with spaces") || !containsShellWord(got, "a'b;rm -rf /") {
		t.Fatalf("values changed instead of quoted: %q", got)
	}
}

func containsShellWord(command, value string) bool {
	return strings.Contains(command, shellQuote(value))
}

func TestOneOffCommandRunsAndRecordsOutput(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	dir := t.TempDir()
	h.app.snapshot.Root = dir
	h.app.commandLog = repository.NewCommandLog()
	h.app.commandPromptText = "printf completed > result.txt; cat result.txt"
	h.app.submitCommandPrompt()
	result := filepath.Join(dir, "result.txt")
	h.waitFor(t, "custom command execution and output recording", func() bool {
		if _, err := os.Stat(result); err != nil {
			return false
		}
		// The output file appears before the process-exit callback appends its
		// command-log entry. Wait for both so this test does not race the callback.
		for _, entry := range h.app.commandLog.Entries() {
			if entry.Command == "printf completed > result.txt; cat result.txt" && entry.Output == "completed" {
				return true
			}
		}
		return false
	})
	if h.app.commandPromptOpen {
		t.Fatal("prompt must close after execution")
	}
	entries := h.app.commandLog.Entries()
	found := false
	for _, entry := range entries {
		if entry.Command == "printf completed > result.txt; cat result.txt" && entry.Output == "completed" {
			found = true
		}
	}
	if !found {
		t.Fatalf("custom command did not reach the command log: %+v", entries)
	}
}

func TestAutoFetchPruneRunsOncePerInterval(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.settings.AutoFetchInterval = 1
	h.app.settings.AutoFetchConfigured = true
	h.app.settings.FetchPrune = true
	h.app.maybeAutoFetch()
	h.waitFor(t, "fetch prune", func() bool { return calls.has("fetch-prune") })
	h.app.maybeAutoFetch()
	if countOf(calls.all(), "fetch-prune") != 1 {
		t.Fatalf("auto fetch duplicated: %v", calls.all())
	}
	h.app.settings.AutoFetchInterval = 0
	before := len(calls.all())
	h.app.maybeAutoFetch()
	if got := len(calls.all()); got != before {
		t.Fatalf("disabled auto-fetch did work: %v", calls.all())
	}
}

func countOf(calls []string, wanted string) int {
	count := 0
	for _, call := range calls {
		if call == wanted {
			count++
		}
	}
	return count
}

type stackedPushSession struct {
	fakeSession
}

func (s *stackedPushSession) StackedBranches(context.Context) ([]string, error) {
	return []string{"one", "two"}, nil
}

func TestPushOffersStackedBranches(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.session = &stackedPushSession{fakeSession: fakeSession{calls: calls, snapshot: h.app.snapshot}}
	h.app.confirmPush()
	h.waitFor(t, "push confirmation", func() bool { return h.app.confirm != nil })
	h.app.acceptConfirm()
	h.waitFor(t, "stacked push", func() bool { return calls.has("push-stack:one,two") })
}

func TestPullRequestActions(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.openCreatePullRequestForm()
	h.app.prTitle = "Fix history"
	h.app.prBody = "Details"
	h.app.prBase = "main"
	h.app.prDraft = true
	h.app.beginCreatePullRequest()
	h.waitFor(t, "pull request creation", func() bool {
		return calls.has("create-pr:Fix history|Details|main|main|true") && !h.app.actionBusy
	})
	h.app.openPullRequestChecks()
	h.waitFor(t, "checks", func() bool {
		return calls.has("pr-checks:main") && h.app.prChecksText == "build  passed  42s"
	})
	if h.app.prChecksText != "build  passed  42s" {
		t.Fatalf("checks text = %q", h.app.prChecksText)
	}
	h.app.prChecksOpen = false
}

func TestBisectStartAndMarksReachSession(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.openBisectStartForm(h.app.snapshot.Commits[0])
	h.app.bisectGoodRef = "v1-good"
	h.app.confirmBisectStart()
	if h.app.confirm == nil {
		t.Fatal("no start confirmation")
	}
	h.app.acceptConfirm()
	h.waitFor(t, "bisect start", func() bool {
		return calls.has("bisect-start:c2c2c2c2:v1-good") && !h.app.actionBusy
	})
	h.app.snapshot.Bisect = &repository.BisectState{Active: true}
	h.app.confirmBisectMark("good", "")
	h.app.acceptConfirm()
	h.waitFor(t, "bisect mark", func() bool {
		return calls.has("bisect-mark:good:") && !h.app.actionBusy
	})
	h.app.snapshot.Bisect = &repository.BisectState{Active: true}
	h.app.confirmBisectReset()
	h.app.acceptConfirm()
	h.waitFor(t, "bisect reset", func() bool { return calls.has("bisect-reset") })
}

func TestEditCommitAction(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.confirmEditCommit(h.app.snapshot.Commits[0])
	if h.app.confirm == nil {
		t.Fatal("no edit confirmation")
	}
	h.app.acceptConfirm()
	h.waitFor(t, "edit commit", func() bool { return calls.has("edit-commit:c2c2c2c2") })
}

func TestRemoteTagDeletionAndPushAll(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.openDeleteRemoteTag(h.app.snapshot.Tags[0])
	h.app.confirmDeleteRemoteTag()
	h.app.acceptConfirm()
	h.waitFor(t, "remote tag deletion", func() bool {
		return calls.has("delete-remote-tag:origin:v2.0.0") && !h.app.actionBusy
	})
	h.app.beginPushAllTags()
	h.app.acceptConfirm()
	h.waitFor(t, "push all tags", func() bool { return calls.has("push-all-tags") })
}

func TestSubmoduleBulkActionsReachSession(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.updateSubmodules(nil, true, false)
	h.waitFor(t, "bulk recursive update", func() bool {
		return calls.has("update-submodules::recursive=true:remote=false") && !h.app.actionBusy
	})
	h.app.updateSubmodules([]string{"libs/one"}, false, true)
	h.waitFor(t, "remote submodule update", func() bool {
		return calls.has("update-submodules:libs/one:recursive=false:remote=true") && !h.app.actionBusy
	})
	h.app.syncSubmodules([]string{"libs/one"})
	h.waitFor(t, "submodule sync", func() bool { return calls.has("sync-submodules:libs/one") })
}

func TestMoveUnpushedCommitsAction(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.openMoveCommitsForm()
	h.app.moveCommitsName = "topic"
	h.app.beginMoveCommitsToNewBranch()
	h.app.acceptConfirm()
	h.waitFor(t, "move commits", func() bool { return calls.has("move-unpushed:topic") })
}

func TestMailboxImportConfirmation(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.confirmMailboxPatchImport("0001-test.patch", []byte("patch"))
	h.app.acceptConfirm()
	h.waitFor(t, "mailbox apply", func() bool { return calls.has("am-patch:0001-test.patch") })
}

// New hosting and command surfaces must not fall back to English when the
// app language changes. Exercise the rendered controls, not just the maps:
// broken catalog registration is invisible to a plain key-count assertion.
func TestNewFeatureSurfacesRenderInEveryLanguage(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	previousLanguage := currentLanguage
	t.Cleanup(func() { currentLanguage = previousLanguage })

	for _, language := range []string{"zh-Hans", "zh-Hant", "ja", "es", "fr"} {
		h.app.settings.Language = language
		h.app.showPRForm = false
		h.app.settingsOpen = false
		h.tester.Frame()

		h.app.openCreatePullRequestForm()
		h.waitFor(t, "hosting preflight", func() bool { return h.app.showPRForm })
		h.tester.Frame()
		title := tr("Create pull request")
		body := tr("Uses gh for GitHub or glab for GitLab with their existing authentication.")
		if !strings.Contains(strings.Join(h.texts(), " "), title) {
			t.Errorf("%s PR form did not use the localized title %q", language, title)
		}
		if !strings.Contains(strings.Join(h.texts(), " "), body) {
			t.Errorf("%s PR explanation was not rendered in the selected language", language)
		}
		h.app.showPRForm = false

		h.app.openSettingsSection(settingsPageCommands)
		h.tester.Frame()
		label := tr("Custom commands")
		if !strings.Contains(strings.Join(h.texts(), " "), label) {
			t.Errorf("%s Commands settings did not use the localized heading %q", language, label)
		}
		h.app.settingsOpen = false
	}
}

// The command and pull-request menus belong to the bottom status island, at
// the status bar's compact button height, not as oversized toolbar controls.
func TestCommandMenusShareTheBottomIsland(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.tester.Frame()
	commands, ok := h.tester.Find("Commands")
	if !ok {
		t.Fatal("commands menu is missing")
	}
	pullRequests, ok := h.tester.Find("Pull requests")
	if !ok {
		t.Fatal("pull request menu is missing")
	}
	commandLog, ok := h.tester.Find("Command log")
	if !ok {
		t.Fatal("bottom command log is missing")
	}
	if absFloat32(commands.Y+commands.H/2-commandLog.Y-commandLog.H/2) > 1 || absFloat32(pullRequests.Y+pullRequests.H/2-commandLog.Y-commandLog.H/2) > 1 {
		t.Fatalf("commands are not on the bottom island: commands=%+v PR=%+v log=%+v", commands, pullRequests, commandLog)
	}
	if commands.H > 27 || pullRequests.H > 27 {
		t.Fatalf("bottom menus do not use compact buttons: commands=%+v PR=%+v", commands, pullRequests)
	}

	if err := h.tester.Click("Commands"); err != nil {
		t.Fatalf("open Commands: %v", err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Run command…"); err != nil {
		t.Fatalf("choose Run command: %v", err)
	}
	h.tester.Frame()
	if !h.app.commandPromptOpen {
		t.Fatal("the command menu did not open its one-off prompt")
	}
	h.app.closeCommandPrompt()
	h.tester.Frame()

	if err := h.tester.Click("Pull requests"); err != nil {
		t.Fatalf("open Pull requests: %v", err)
	}
	h.tester.Frame()
	if err := h.tester.Click("Create pull request…"); err != nil {
		t.Fatalf("choose Create pull request: %v", err)
	}
	h.waitFor(t, "hosting preflight", func() bool { return h.app.showPRForm })
	h.tester.Frame()
	if !h.app.showPRForm {
		t.Fatal("the pull request menu is disabled even with a branch and a remote")
	}
	h.app.showPRForm = false
}

func absFloat32(value float32) float32 {
	if value < 0 {
		return -value
	}
	return value
}

type hostingFeedbackSession struct {
	fakeSession
	setupErr error
}

func (s *hostingFeedbackSession) CheckPullRequestReady(context.Context) error {
	return s.setupErr
}

func TestHelpingWithHostingSetupDoesNotRequireAuthFirst(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.session = &hostingFeedbackSession{
		fakeSession: fakeSession{snapshot: h.app.snapshot, calls: calls},
		setupErr:    repository.ErrPullRequestSetup,
	}
	h.app.openCreatePullRequestForm()
	h.waitFor(t, "setup guidance", func() bool {
		return h.tester.HasText("Set up gh for pull requests")
	})
	if h.app.showPRForm {
		t.Fatal("PR form opened without authenticated CLI")
	}
	if !h.tester.HasText("Install gh or sign in with 'gh auth login', then retry. The CLI output is in the command log.") {
		t.Fatalf("missing actionable auth setup text: %q", h.texts())
	}
	if !h.tester.HasText("View log") {
		t.Fatalf("missing command-log action: %q", h.texts())
	}
}

type missingPullRequestSession struct {
	fakeSession
}

func (s *missingPullRequestSession) PullRequestChecks(context.Context, string) (string, error) {
	return "", repository.ErrPullRequestMissing
}

func TestMissingPullRequestOffersCreationInsteadOfSetup(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	h.app.session = &missingPullRequestSession{fakeSession{snapshot: h.app.snapshot, calls: calls}}
	h.app.openPullRequestChecks()
	h.waitFor(t, "missing-PR feedback", func() bool {
		return h.tester.HasText("No pull request for this branch")
	})
	if h.app.prChecksOpen {
		t.Fatal("a missing PR left an empty error dialog open")
	}
	if h.tester.HasText("Set up gh for pull requests") {
		t.Fatal("missing PR was incorrectly described as a CLI setup failure")
	}
	if err := h.tester.Click("Create pull request"); err != nil {
		t.Fatalf("missing PR did not offer creation: %v", err)
	}
	h.waitFor(t, "offered PR creation", func() bool { return h.app.showPRForm })
}

func TestPlaceholderExpansionDoesNotReexpandValues(t *testing.T) {
	got := customCommandPlaceholders("echo {{branch}}", map[string]string{
		"branch": "{{file}}",
		"file":   "$(printf INJECTED)",
	})
	want := "echo '{{file}}'"
	if got != want {
		t.Fatalf("branches were expanded again after substitution: got %q, want %q", got, want)
	}
}

func TestCommandPlaceholdersPreserveLiteralSubstitution(t *testing.T) {
	cmd := customCommandPlaceholders("printf '%s' {{branch}}", map[string]string{
		"branch": "prefix$(printf INJECTED)",
	})
	if cmd != "printf '%s' 'prefix$(printf INJECTED)'" {
		t.Fatalf("did not preserve literal branch text: %q", cmd)
	}
}

func TestCustomShortcutOverridesBuiltInBinding(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.app.snapshot.Root = t.TempDir()
	h.app.settings.CustomCommands = []customCommandSetting{{
		Name: "local marker", Command: "touch local-marker", Key: "p", Contexts: []string{"everywhere"},
	}}
	h.press(t, ui.KeyP)
	marker := filepath.Join(h.app.snapshot.Root, "local-marker")
	h.waitFor(t, "custom p shortcut", func() bool {
		_, err := os.Stat(marker)
		return err == nil
	})
	if h.app.confirm != nil {
		t.Fatal("built-in pull handled p instead of the custom command")
	}
}

type blockingStackDiscoverySession struct {
	fakeSession
	release chan struct{}
}

func (s *blockingStackDiscoverySession) StackedBranches(ctx context.Context) ([]string, error) {
	select {
	case <-s.release:
		return []string{"base"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestStackDiscoveryDoesNotBlockUIShowingConfirmation(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	release := make(chan struct{}, 1)
	h.app.session = &blockingStackDiscoverySession{fakeSession: fakeSession{snapshot: h.app.snapshot, calls: calls}, release: release}
	started := time.Now()
	h.app.confirmPush()
	if elapsed := time.Since(started); elapsed > 500*time.Millisecond {
		close(release)
		t.Fatalf("stack discovery blocked the UI for %v", elapsed)
	}
	select {
	case release <- struct{}{}:
	default:
	}
	h.waitFor(t, "push confirmation", func() bool { return h.app.confirm != nil })
	h.confirmAttentionForTest(t)
	h.waitFor(t, "stack push", func() bool { return calls.has("push-stack:base") && !h.app.actionBusy })
}

func TestOpenPullRequestUsesLocalBrowserForBranch(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	opened := ""
	h.app.openExternalURL = func(url string) { opened = url }
	h.app.confirmOpenPullRequestReady()
	h.confirmAttentionForTest(t)
	h.waitFor(t, "open local PR", func() bool { return opened != "" && calls.has("open-pr:main") && !h.app.actionBusy })
	if got := opened; got != "https://example.com/pull/42" {
		t.Fatalf("opened %q, want the exact resolved PR URL", got)
	}
}

func TestGitCommandLogOnErrorDoesNotStartInUserSection(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.app.commandLogOpen = false
	h.app.notifyError(errors.New("git refused operation"))
	if len(h.app.pendingToasts) == 0 {
		t.Fatal("expected actionable error toast")
	}
	h.app.pendingToasts[0].onAction()
	if h.app.commandLogUser {
		t.Fatal("Git error opens the wrong command-log tab")
	}
	if !h.app.commandLogVisible.Load() {
		t.Fatal("live log listener was not marked visible")
	}
}

func TestOpeningCLILogIsSeparateFromUserCommands(t *testing.T) {
	a := newApp(fakeOpener{})
	logger := repository.NewCommandLog()
	a.commandLog = logger
	logger.BeginOrigin("custom external command", repository.CommandOriginUser)("custom", false)
	logger.BeginOrigin("git fetch", repository.CommandOriginGit)("fetch", false)
	a.openCommandLog()
	userEntries := a.commandLogVisibleEntries()
	a.openGitCommandLog()
	gitEntries := a.commandLogVisibleEntries()
	if len(userEntries) != 1 || userEntries[0].Command != "custom external command" {
		t.Fatalf("user entries = %+v", userEntries)
	}
	if len(gitEntries) != 1 || gitEntries[0].Command != "git fetch" {
		t.Fatalf("git entries = %+v", gitEntries)
	}
}

func TestCustomF5OverridesGlobalRefresh(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.app.snapshot.Root = t.TempDir()
	h.app.settings.CustomCommands = []customCommandSetting{{
		Name: "custom refresh key", Command: "touch custom-f5", Key: "f5", Contexts: []string{"everywhere"},
	}}
	before := h.app.refreshToken
	h.press(t, ui.KeyF5)
	marker := filepath.Join(h.app.snapshot.Root, "custom-f5")
	h.waitFor(t, "F5 custom command", func() bool {
		_, err := os.Stat(marker)
		return err == nil
	})
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("custom F5 command did not run: %v; initial refresh token=%d current=%d", err, before, h.app.refreshToken)
	}
}

func TestCustomCtrlLOpensUserCommandNotBuiltinLog(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.app.snapshot.Root = t.TempDir()
	h.app.settings.CustomCommands = []customCommandSetting{{
		Name: "custom ctrl-l", Command: "touch custom-ctrl-l", Key: "ctrl+l", Contexts: []string{"everywhere"},
	}}
	h.tester.Key(ui.Ctrl, ui.KeyL)
	h.tester.Frame()
	h.settle(t)
	marker := filepath.Join(h.app.snapshot.Root, "custom-ctrl-l")
	h.waitFor(t, "custom Ctrl+L", func() bool {
		_, err := os.Stat(marker)
		return err == nil
	})
	if h.app.commandLogOpen {
		t.Fatal("Ctrl+L opened built-in command log instead of the user command")
	}
}

func TestCustomCtrlSPrecedesCodeSaveAndDoesNotSave(t *testing.T) {
	h, _ := menuTestAppWithCalls(t)
	h.app.workspaceTab = sectionCode
	path := filepath.Join(t.TempDir(), "original.txt")
	h.app.snapshot.Root = filepath.Dir(path)
	state := &editor.State{Buffer: editor.NewBuffer("original")}
	state.Insert("dirty")
	h.app.code.documents = map[string]*codeDocument{"original.txt": {path: path, state: state}}
	h.app.code.active = "original.txt"
	h.app.code.tabs = []string{"original.txt"}
	binding := "ctrl+s"
	if ui.Cmd != ui.Ctrl {
		binding = "super+s"
	}
	h.app.settings.CustomCommands = []customCommandSetting{{
		Name: "custom save key", Command: "touch custom-ctrl-s", Key: binding, Contexts: []string{"everywhere"},
	}}
	h.tester.Frame()
	h.tester.Key(ui.Cmd, ui.KeyS)
	h.tester.Frame()
	marker := filepath.Join(h.app.snapshot.Root, "custom-ctrl-s")
	h.waitFor(t, "custom Ctrl+S", func() bool {
		_, err := os.Stat(marker)
		return err == nil
	})
	if !state.Buffer.Modified() {
		t.Fatal("the built-in save consumed Ctrl+S before the custom binding")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the built-in save wrote the dirty document: %v", err)
	}
}
