package app

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
)

type agentIntegrationDeleteCall struct {
	path  string
	force bool
}

type agentIntegrationTestSession struct {
	fakeSession
	mu           sync.Mutex
	taskSnapshot repository.Snapshot
	targetStatus repository.AgentTargetStatus
	statusErr    error
	resolveSHA   string
	createCalls  []repository.WorktreeCreateOptions
	mergeCalls   []string
	mergeErrors  map[string]error
	deleteCalls  []agentIntegrationDeleteCall
	statusInputs []string
	closeCalls   int
}

func (s *agentIntegrationTestSession) Snapshot(context.Context) (repository.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.taskSnapshot, nil
}

func (s *agentIntegrationTestSession) AgentTargetStatus(_ context.Context, base string) (repository.AgentTargetStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusInputs = append(s.statusInputs, base)
	return s.targetStatus, s.statusErr
}

func (s *agentIntegrationTestSession) ResolveCommit(context.Context, string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resolveSHA, nil
}

func (s *agentIntegrationTestSession) CreateWorktree(_ context.Context, options repository.WorktreeCreateOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createCalls = append(s.createCalls, options)
	return nil
}

func (s *agentIntegrationTestSession) MergeBranch(_ context.Context, captured string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mergeCalls = append(s.mergeCalls, captured)
	return s.mergeErrors[captured]
}

func (s *agentIntegrationTestSession) DeleteWorktree(_ context.Context, path string, force bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleteCalls = append(s.deleteCalls, agentIntegrationDeleteCall{path: path, force: force})
	return nil
}

func (s *agentIntegrationTestSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeCalls++
	return nil
}

type agentIntegrationTestOpener struct {
	mu          sync.Mutex
	sessions    map[string]*agentIntegrationTestSession
	integration *agentIntegrationTestSession
	opens       []string
	openHook    func(string)
}

func (o *agentIntegrationTestOpener) Open(_ context.Context, path string) (repository.Session, error) {
	o.mu.Lock()
	o.opens = append(o.opens, path)
	session := o.sessions[path]
	if session == nil && o.integration != nil && strings.Contains(filepath.Base(path), "tangit-agent-integration-") {
		session = o.integration
	}
	hook := o.openHook
	o.mu.Unlock()
	if hook != nil {
		hook(path)
	}
	if session == nil {
		return nil, errors.New("unknown explicit integration path " + path)
	}
	return session, nil
}

func (o *agentIntegrationTestOpener) Init(context.Context, string) error { return nil }

func (o *agentIntegrationTestOpener) openedPaths() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.opens...)
}

func agentIntegrationTask(repositoryID, id, title, path, base, branch string, dependencies ...string) agentwork.Task {
	return agentwork.Task{
		ID: id, RepositoryID: repositoryID, Title: title, WorktreePath: path,
		BaseRef: branch, BaseCommit: base, Branch: branch, DependsOn: dependencies,
		Status: agentwork.StatusExited,
	}
}

func agentIntegrationTarget(head, base string, files ...repository.File) *agentIntegrationTestSession {
	return &agentIntegrationTestSession{
		taskSnapshot: repository.Snapshot{HeadHash: head, Files: files},
		targetStatus: repository.AgentTargetStatus{HeadSHA: head, BaseSHA: base},
	}
}

func (o *agentIntegrationTestOpener) prepare(h *harness, source *agentIntegrationTestSession, sessions map[string]*agentIntegrationTestSession) {
	if h.app.session == nil {
		h.app.session = fakeSession{}
	}
	if o.sessions == nil {
		o.sessions = make(map[string]*agentIntegrationTestSession)
	}
	o.sessions[h.app.agentIntegrationFamilyRoot()] = source
	for path, session := range sessions {
		o.sessions[path] = session
	}
	h.app.opener = o
}

func TestAgentIntegrationConfirmationOrderCapturedSHAsAndInspectableSuccess(t *testing.T) {
	h, _ := newAgentTestHarness(t)
	repositoryID := h.app.agentRepositoryID()
	dependent := agentIntegrationTask(repositoryID, "11111111111111111111111111111111", "Dependent", "/work/dependent", "base-dependent", "agent/dependent", "22222222222222222222222222222222")
	dependency := agentIntegrationTask(repositoryID, "22222222222222222222222222222222", "Dependency", "/work/dependency", "base-dependency", "agent/dependency")
	independent := agentIntegrationTask(repositoryID, "33333333333333333333333333333333", "Independent", "/work/independent", "base-independent", "agent/independent")
	archived := agentIntegrationTask(repositoryID, "44444444444444444444444444444444", "Archived", "/work/archived", "base-archived", "agent/archived")
	archived.Status = agentwork.StatusArchived
	h.app.agents.tasks = []agentwork.Task{independent, archived, dependent, dependency}

	source := &agentIntegrationTestSession{resolveSHA: "immutable-base-sha"}
	dependencyTarget := agentIntegrationTarget("captured-dependency-sha", "resolved-base-dependency")
	dependentTarget := agentIntegrationTarget("captured-dependent-sha", "resolved-base-dependent")
	independentTarget := agentIntegrationTarget("captured-independent-sha", "resolved-base-independent")
	integration := &agentIntegrationTestSession{}
	opener := &agentIntegrationTestOpener{integration: integration}
	opener.prepare(h, source, map[string]*agentIntegrationTestSession{
		dependency.WorktreePath:  dependencyTarget,
		dependent.WorktreePath:   dependentTarget,
		independent.WorktreePath: independentTarget,
	})

	h.app.beginAgentIntegrationDryRun()
	if !h.app.agentIntegration.open || len(h.app.agentIntegration.ordered) != 3 {
		t.Fatalf("selection did not open with exactly the active tasks: %#v", h.app.agentIntegration)
	}
	for _, selected := range h.app.agentIntegration.selected {
		if selected == nil || !*selected {
			t.Fatal("eligible active tasks were not selected by default")
		}
	}
	if got := len(opener.openedPaths()); got != 0 {
		t.Fatalf("opening selection unexpectedly opened %d sessions", got)
	}
	h.app.beginAgentIntegrationDryRun()
	if h.app.confirm == nil {
		t.Fatal("explicit destructive/attention confirmation was not requested")
	}
	if len(source.createCalls) != 0 || len(integration.mergeCalls) != 0 {
		t.Fatal("a Git write happened before confirmation")
	}
	h.app.acceptConfirm()
	h.waitFor(t, "integration dry run", func() bool { return h.app.agentIntegration.completed && !h.app.agentIntegration.running })

	state := h.app.agentIntegration
	if state.err != "" || !state.succeeded || state.stopped {
		t.Fatalf("dry run failed: %#v", state)
	}
	if len(source.createCalls) != 1 {
		t.Fatalf("create calls = %#v", source.createCalls)
	}
	created := source.createCalls[0]
	if created.Base != "immutable-base-sha" || !created.Detach || created.Branch != "" || created.Force {
		t.Fatalf("integration worktree was not immutable/detached: %#v", created)
	}
	if filepath.Dir(created.Path) != "/work" || created.Path == dependency.WorktreePath || created.Path == h.app.snapshot.Root {
		t.Fatalf("integration path is not a unique sibling: %q", created.Path)
	}
	if !state.worktreeCreated || state.worktreePath != created.Path || state.baseCommit != "immutable-base-sha" {
		t.Fatalf("inspectable result lost its captured base/path: %#v", state)
	}
	wantMerges := []string{"captured-dependency-sha", "captured-dependent-sha", "captured-independent-sha"}
	if !reflect.DeepEqual(integration.mergeCalls, wantMerges) {
		t.Fatalf("merge calls = %#v, want dependency-first captured SHAs %#v", integration.mergeCalls, wantMerges)
	}
	for _, forbidden := range []string{dependency.Branch, dependent.Branch, independent.Branch} {
		for _, merged := range integration.mergeCalls {
			if merged == forbidden {
				t.Fatalf("branch name %q was merged instead of a captured SHA", forbidden)
			}
		}
	}
	if len(state.steps) != 3 {
		t.Fatalf("per-step results = %#v", state.steps)
	}
	if dependent.BaseCommit != "base-dependent" || h.app.agents.tasks[2].Status != agentwork.StatusExited {
		t.Fatal("dry run mutated task BaseCommit or readiness")
	}
	opened := strings.Join(opener.openedPaths(), "\n")
	for _, exact := range []string{dependency.WorktreePath, dependent.WorktreePath, independent.WorktreePath, created.Path} {
		if !strings.Contains(opened, exact) {
			t.Fatalf("exact session path %q was not opened: %s", exact, opened)
		}
	}
	if !reflect.DeepEqual(dependencyTarget.statusInputs, []string{"base-dependency"}) ||
		!reflect.DeepEqual(dependentTarget.statusInputs, []string{"base-dependent"}) ||
		!reflect.DeepEqual(independentTarget.statusInputs, []string{"base-independent"}) {
		t.Fatalf("captured bases used the wrong immutable task values")
	}

	tester := ui.NewTester(func(c *ui.Context) { h.app.agentIntegrationModal(c) }, 1200, 850)
	tester.Frame()
	text := strings.Join(tester.Texts(), "\n")
	for _, visible := range []string{"DRY RUN", "Immutable integration BASE SHA: immutable-base-sha", "CAPTURED_HEAD captured-dependent-sha", "KEPT FOR INSPECTION"} {
		if !strings.Contains(text, visible) {
			t.Fatalf("modal does not show %q:\n%s", visible, text)
		}
	}
}

func TestAgentIntegrationExcludesDirtyDependencyAndDependent(t *testing.T) {
	h, _ := newAgentTestHarness(t)
	repositoryID := h.app.agentRepositoryID()
	dependency := agentIntegrationTask(repositoryID, "11111111111111111111111111111111", "Dirty dependency", "/work/dirty", "base-dirty", "agent/dirty")
	dependent := agentIntegrationTask(repositoryID, "22222222222222222222222222222222", "Clean dependent", "/work/dependent", "base-dependent", "agent/dependent", dependency.ID)
	h.app.agents.tasks = []agentwork.Task{dependent, dependency}
	files := []repository.File{
		{Path: "staged.go", Staged: true, Tracked: true},
		{Path: "modified.go", Unstaged: true, Tracked: true},
		{Path: "untracked.go", Unstaged: true, Tracked: false},
	}
	dirty := agentIntegrationTarget("captured-dirty-head", "resolved-base-dirty", files...)
	clean := agentIntegrationTarget("captured-dependent-head", "resolved-base-dependent")
	source := &agentIntegrationTestSession{resolveSHA: "base"}
	opener := &agentIntegrationTestOpener{integration: &agentIntegrationTestSession{}}
	opener.prepare(h, source, map[string]*agentIntegrationTestSession{dependency.WorktreePath: dirty, dependent.WorktreePath: clean})

	h.app.beginAgentIntegrationDryRun()
	h.app.beginAgentIntegrationDryRun()
	h.app.acceptConfirm()
	h.waitFor(t, "dirty exclusions", func() bool { return h.app.agentIntegration.completed })
	state := h.app.agentIntegration
	if len(state.exclusions) != 2 {
		t.Fatalf("dirty dependency chain exclusions = %#v", state.exclusions)
	}
	if !strings.Contains(state.exclusions[0].Reason, "1 staged, 1 unstaged, 1 untracked") ||
		!strings.Contains(state.exclusions[1].Reason, "excluded or unavailable") {
		t.Fatalf("unclear dirty/dependency exclusions: %#v", state.exclusions)
	}
	if len(state.captured) != 2 || state.captured[0].CapturedHead != "captured-dirty-head" {
		t.Fatalf("dirty task HEAD was not reported as captured data: %#v", state.captured)
	}
	if len(source.createCalls) != 0 || len(opener.integration.mergeCalls) != 0 || state.worktreeCreated {
		t.Fatal("dirty/excluded tasks caused an integration write")
	}
}

func TestAgentIntegrationRequiresExplicitDependencySelection(t *testing.T) {
	h, _ := newAgentTestHarness(t)
	repositoryID := h.app.agentRepositoryID()
	dependency := agentIntegrationTask(repositoryID, "11111111111111111111111111111111", "Dependency", "/work/dependency", "base-dependency", "agent/dependency")
	dependent := agentIntegrationTask(repositoryID, "22222222222222222222222222222222", "Dependent", "/work/dependent", "base-dependent", "agent/dependent", dependency.ID)
	h.app.agents.tasks = []agentwork.Task{dependency, dependent}
	source := &agentIntegrationTestSession{resolveSHA: "base"}
	opener := &agentIntegrationTestOpener{integration: &agentIntegrationTestSession{}}
	opener.prepare(h, source, map[string]*agentIntegrationTestSession{
		dependency.WorktreePath: agentIntegrationTarget("dependency-head", "resolved-dependency"),
		dependent.WorktreePath:  agentIntegrationTarget("dependent-head", "resolved-dependent"),
	})

	h.app.beginAgentIntegrationDryRun()
	*h.app.agentIntegration.selected[dependency.ID] = false
	h.app.beginAgentIntegrationDryRun()
	h.app.acceptConfirm()
	h.waitFor(t, "explicit dependency exclusion", func() bool { return h.app.agentIntegration.completed })
	if len(h.app.agentIntegration.exclusions) != 1 || !strings.Contains(h.app.agentIntegration.exclusions[0].Reason, "not explicitly selected") {
		t.Fatalf("missing dependency was implicitly authorized: %#v", h.app.agentIntegration.exclusions)
	}
	if len(source.createCalls) != 0 || len(opener.integration.mergeCalls) != 0 {
		t.Fatal("dependent ran without its explicitly selected dependency")
	}
	if len(opener.openedPaths()) != 2 { // family source and selected dependent only
		t.Fatalf("unselected dependency session was opened: %#v", opener.openedPaths())
	}
}

func TestAgentIntegrationStopsAtFirstConflictAndRequiresConfirmedCleanup(t *testing.T) {
	h, _ := newAgentTestHarness(t)
	repositoryID := h.app.agentRepositoryID()
	first := agentIntegrationTask(repositoryID, "11111111111111111111111111111111", "First", "/work/first", "base-first", "agent/first")
	second := agentIntegrationTask(repositoryID, "22222222222222222222222222222222", "Second", "/work/second", "base-second", "agent/second")
	third := agentIntegrationTask(repositoryID, "33333333333333333333333333333333", "Third", "/work/third", "base-third", "agent/third")
	h.app.agents.tasks = []agentwork.Task{third, second, first}
	source := &agentIntegrationTestSession{resolveSHA: "base"}
	integration := &agentIntegrationTestSession{mergeErrors: map[string]error{"head-second": errors.New("CONFLICT: shared.go")}}
	opener := &agentIntegrationTestOpener{integration: integration}
	opener.prepare(h, source, map[string]*agentIntegrationTestSession{
		first.WorktreePath:  agentIntegrationTarget("head-first", "resolved-first"),
		second.WorktreePath: agentIntegrationTarget("head-second", "resolved-second"),
		third.WorktreePath:  agentIntegrationTarget("head-third", "resolved-third"),
	})

	h.app.beginAgentIntegrationDryRun()
	h.app.beginAgentIntegrationDryRun()
	h.app.acceptConfirm()
	h.waitFor(t, "conflicted dry run", func() bool { return h.app.agentIntegration.completed })
	state := h.app.agentIntegration
	if !state.stopped || state.succeeded || !state.worktreeCreated {
		t.Fatalf("conflict was not retained as a stopped dry run: %#v", state)
	}
	if !reflect.DeepEqual(integration.mergeCalls, []string{"head-first", "head-second"}) {
		t.Fatalf("merge did not stop at first conflict: %#v", integration.mergeCalls)
	}
	if len(state.steps) != 3 || state.steps[0].Status != agentIntegrationMerged ||
		state.steps[1].Status != agentIntegrationStopped || state.steps[2].Status != agentIntegrationNotRun {
		t.Fatalf("per-step conflict results = %#v", state.steps)
	}
	if len(source.deleteCalls) != 0 {
		t.Fatal("conflicted worktree was silently deleted")
	}
	h.app.confirmAgentIntegrationCleanup()
	if h.app.confirm == nil || len(source.deleteCalls) != 0 {
		t.Fatal("cleanup did not remain behind explicit confirmation")
	}
	h.app.acceptConfirm()
	h.waitFor(t, "confirmed integration cleanup", func() bool { return h.app.agentIntegration.cleanupDone && !h.app.agentIntegration.cleaning })
	if len(source.deleteCalls) != 1 || source.deleteCalls[0].path != state.worktreePath || !source.deleteCalls[0].force {
		t.Fatalf("explicit cleanup calls = %#v", source.deleteCalls)
	}
	if h.app.agentIntegration.worktreeCreated || !h.app.agentIntegration.cleanupDone {
		t.Fatal("cleanup did not preserve the report while marking the worktree removed")
	}
}

func TestAgentIntegrationResetRejectsStaleCompletion(t *testing.T) {
	h, _ := newAgentTestHarness(t)
	repositoryID := h.app.agentRepositoryID()
	task := agentIntegrationTask(repositoryID, "11111111111111111111111111111111", "Slow capture", "/work/slow", "base-slow", "agent/slow")
	h.app.agents.tasks = []agentwork.Task{task}
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	source := &agentIntegrationTestSession{resolveSHA: "base"}
	opener := &agentIntegrationTestOpener{
		integration: &agentIntegrationTestSession{},
		openHook: func(path string) {
			if path == task.WorktreePath {
				once.Do(func() { close(entered) })
				<-release
			}
		},
	}
	opener.prepare(h, source, map[string]*agentIntegrationTestSession{task.WorktreePath: agentIntegrationTarget("slow-head", "resolved-slow")})

	h.app.beginAgentIntegrationDryRun()
	h.app.beginAgentIntegrationDryRun()
	h.app.acceptConfirm()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("slow task session was not opened")
	}
	staleToken := h.app.agentIntegration.token
	h.app.resetAgentIntegration()
	close(release)
	h.updates.waitForUpdate(t)
	if h.app.agentIntegration.token == staleToken || h.app.agentIntegration.completed || h.app.agentIntegration.running {
		t.Fatalf("stale completion landed after reset: %#v", h.app.agentIntegration)
	}
	if len(source.createCalls) != 0 || len(opener.integration.mergeCalls) != 0 || len(source.deleteCalls) != 0 {
		t.Fatal("stale operation wrote or deleted after reset")
	}
}
