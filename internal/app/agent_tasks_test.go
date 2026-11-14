package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
	"lazygitapp/internal/workspace"
)

func newAgentTestHarness(t *testing.T) (*harness, *agentwork.Store) {
	t.Helper()
	a := newApp(fakeOpener{})
	a.state = stateReady
	a.workspaceTab = sectionAgents
	a.snapshot.Root = "/work/project"
	a.location = workspace.Location{Kind: workspace.Local, Root: "/work/project"}
	store, err := agentwork.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	a.agentStore = store
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1280, 800)
	a.update = h.updates.update
	return h, store
}

func createAgentTestTask(t *testing.T, h *harness, store *agentwork.Store, task agentwork.Task) agentwork.Task {
	t.Helper()
	if task.RepositoryID == "" {
		task.RepositoryID = h.app.agentRepositoryID()
	}
	if task.Title == "" {
		task.Title = "Implement repository search"
	}
	if task.BaseCommit == "" {
		task.BaseCommit = "0123456789abcdef"
	}
	if task.Branch == "" {
		task.Branch = "agent/repository-search"
	}
	if task.WorktreePath == "" {
		task.WorktreePath = "/work/agent-search"
	}
	created, err := store.Create(task)
	if err != nil {
		t.Fatal(err)
	}
	h.app.agents.tasks = []agentwork.Task{created.Clone()}
	h.app.agents.selectedTaskID = created.ID
	return created
}

func TestAgentRepositoryIDSharedByMainAndLinkedWorktrees(t *testing.T) {
	worktrees := []repository.Worktree{
		{Name: "search", Path: "/work/project-search", Branch: "agent/search", Current: true},
		{Name: "project", Path: "/work/project", Branch: "main", Main: true},
	}
	for _, test := range []struct {
		name string
		root string
	}{
		{name: "main worktree", root: "/work/project"},
		{name: "linked worktree", root: "/work/project-search"},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := newApp(fakeOpener{})
			a.snapshot.Root = test.root
			a.snapshot.Worktrees = worktrees
			a.location = workspace.Location{Kind: workspace.Local, Root: test.root}
			if got, want := a.agentRepositoryID(), "/work/project"; got != want {
				t.Fatalf("repository identity = %q, want shared main-worktree identity %q", got, want)
			}
		})
	}
}

func TestAgentTaskCommandQuotesSubstitutionsAndExpandsOnlyOnce(t *testing.T) {
	t.Setenv("SHELL", "/bin/agent test shell")
	task := agentwork.Task{
		Title:        `title {request}`,
		Request:      `request {worktree} $(printf dangerous) 'quoted'`,
		BaseCommit:   `abc123 {task}`,
		Branch:       `agent/topic {request}`,
		WorktreePath: `/work/agent tree/it's {task}`,
		SessionID:    `session {base}`,
		Profile: agentwork.Profile{
			LaunchCommand: "agent run --worktree {worktree} --task {{task}} --request {request} --prompt {promptFile} --base {base} --branch {{branch}} --session {sessionId}",
			ResumeCommand: "agent resume {{sessionId}} --request {request}",
			SetupCommand:  "setup --prepare",
		},
	}
	requestPath := `/tmp/request file {task}.md`
	a := newApp(fakeOpener{})

	expanded := "agent run --worktree " + shellQuote(task.WorktreePath) +
		" --task " + shellQuote(task.Title) +
		" --request " + shellQuote(task.Request) +
		" --prompt " + shellQuote(requestPath) +
		" --base " + shellQuote(task.BaseCommit) +
		" --branch " + shellQuote(task.Branch) +
		" --session " + shellQuote(task.SessionID)
	wantLaunch := []string{"/bin/agent test shell", "-lc", "(setup --prepare) && (" + expanded + ")"}
	if got := a.agentTaskCommand(task, false, requestPath); !slices.Equal(got, wantLaunch) {
		t.Fatalf("launch command = %#v, want %#v", got, wantLaunch)
	}
	// Check one substitution literally as well as the complete command: quotes
	// and shell syntax in task data stay quoted, and {task} inside the worktree
	// is not expanded a second time.
	if got := a.agentTaskCommand(task, false, requestPath)[2]; !strings.Contains(got, `'/work/agent tree/it'\''s {task}'`) {
		t.Fatalf("worktree was not quoted and expanded exactly once: %q", got)
	}

	wantResume := []string{"/bin/agent test shell", "-lc", "agent resume " + shellQuote(task.SessionID) + " --request " + shellQuote(task.Request)}
	if got := a.agentTaskCommand(task, true, requestPath); !slices.Equal(got, wantResume) {
		t.Fatalf("resume command = %#v, want %#v", got, wantResume)
	}
}
