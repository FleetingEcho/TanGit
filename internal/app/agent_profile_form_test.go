package app

import (
	"context"
	"errors"
	"slices"
	"testing"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
)

type agentProfileCreateSession struct {
	fakeSession
	options chan repository.WorktreeCreateOptions
}

func (s *agentProfileCreateSession) CreateWorktree(_ context.Context, options repository.WorktreeCreateOptions) error {
	s.options <- options
	return nil
}

type agentProfileOpener struct {
	session repository.Session
	err     error
}

func (o agentProfileOpener) Open(context.Context, string) (repository.Session, error) {
	if o.err != nil {
		return nil, o.err
	}
	return o.session, nil
}
func (o agentProfileOpener) Init(context.Context, string) error { return nil }

type agentProfileCopyErrorSession struct{ fakeSession }

func (s agentProfileCopyErrorSession) CopyAgentIgnoredFiles(_ context.Context, source string, paths []string) error {
	s.fakeSession.CopyAgentIgnoredFiles(context.Background(), source, paths)
	return errors.New("copy refused for test")
}

func configureAgentProfileTaskForm(h *harness) {
	h.app.agents.title = "Isolated service"
	h.app.agents.request = "Implement the service"
	h.app.agents.base = "main"
	h.app.agents.branch = "agent/isolated-service"
	h.app.agents.path = "/work/isolated-service"
	h.app.agents.launchCommand = "agent --prompt {promptFile}"
	h.app.agents.resumeCommand = "agent resume {sessionId}"
	h.app.agents.setupCommand = "setup"
	h.app.agents.testCommand = "test --port {port}"
	h.app.agents.runCommand = "run --database {database}"
	h.app.agents.environment = "LOG_LEVEL=debug\nFEATURE_FLAG=enabled"
	h.app.agents.sparsePaths = "cmd/service\n internal/shared "
	h.app.agents.ignoredFiles = ".env\n.cache/config.json"
	h.app.agents.developmentPort = "4310"
	h.app.agents.databaseName = "task_service"
	h.app.agents.cacheDirectory = ".cache/service"
}

func TestAgentTaskCreationPersistsFullProfileAndInitializesExplicitSetup(t *testing.T) {
	h, store := newAgentTestHarness(t)
	dependency := createAgentTestTask(t, h, store, agentwork.Task{Title: "Schema first"})
	calls := &callRecorder{}
	h.app.snapshot.HeadHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	creating := &agentProfileCreateSession{
		fakeSession: fakeSession{snapshot: h.app.snapshot, calls: calls},
		options:     make(chan repository.WorktreeCreateOptions, 1),
	}
	h.app.session = creating
	h.app.opener = agentProfileOpener{session: fakeSession{snapshot: h.app.snapshot, calls: calls}}
	configureAgentProfileTaskForm(h)
	selected := true
	h.app.agents.dependencies = map[string]*bool{dependency.ID: &selected}

	h.app.beginCreateAgentTask()
	h.waitFor(t, "full-profile agent task creation", func() bool {
		return !h.app.actionBusy && len(h.app.agents.tasks) == 2
	})
	options := <-creating.options
	if !slices.Equal(options.SparsePaths, []string{"cmd/service", "internal/shared"}) {
		t.Fatalf("sparse paths = %#v", options.SparsePaths)
	}
	if !calls.has("copy-agent-ignored:/work/project:.env,.cache/config.json") {
		t.Fatalf("explicit ignored files were not copied from the source worktree: %#v", calls.all())
	}
	tasks, err := store.List(h.app.agentRepositoryID())
	if err != nil {
		t.Fatal(err)
	}
	var created agentwork.Task
	for _, task := range tasks {
		if task.Branch == "agent/isolated-service" {
			created = task
		}
	}
	if created.ID == "" || !slices.Equal(created.DependsOn, []string{dependency.ID}) {
		t.Fatalf("created task identity/dependencies = %#v", created)
	}
	profile := created.Profile
	if profile.LaunchCommand != "agent --prompt {promptFile}" || profile.RunCommand != "run --database {database}" ||
		profile.Environment["LOG_LEVEL"] != "debug" || profile.Resources.DevelopmentPort != 4310 ||
		profile.Resources.DatabaseName != "task_service" || profile.Resources.CacheDirectory != ".cache/service" ||
		!slices.Equal(profile.IgnoredFiles, []string{".env", ".cache/config.json"}) {
		t.Fatalf("created profile = %#v", profile)
	}
}

func TestAgentTaskCreationKeepsDurableTaskWhenIgnoredFileSetupFails(t *testing.T) {
	h, store := newAgentTestHarness(t)
	calls := &callRecorder{}
	h.app.snapshot.HeadHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	creating := &agentProfileCreateSession{
		fakeSession: fakeSession{snapshot: h.app.snapshot, calls: calls},
		options:     make(chan repository.WorktreeCreateOptions, 1),
	}
	h.app.session = creating
	h.app.opener = agentProfileOpener{session: agentProfileCopyErrorSession{fakeSession{calls: calls}}}
	configureAgentProfileTaskForm(h)
	h.app.agents.launchAfterAdd = true

	h.app.beginCreateAgentTask()
	h.waitFor(t, "partially initialized agent task", func() bool {
		return !h.app.actionBusy && len(h.app.agents.tasks) == 1
	})
	tasks, err := store.List(h.app.agentRepositoryID())
	if err != nil || len(tasks) != 1 {
		t.Fatalf("persisted tasks = %#v, %v", tasks, err)
	}
	if len(h.app.term.sessions) != 0 {
		t.Fatal("agent launched even though explicit setup was incomplete")
	}
}

func TestAgentWorkspaceTemplateCRUDAndApply(t *testing.T) {
	h, store := newAgentTestHarness(t)
	h.app.beginNewAgentTask()
	configureAgentProfileTaskForm(h)
	h.app.agents.templateName = "Go isolated service"
	h.app.saveAgentTemplate()
	if h.app.agents.templatesBusy {
		t.Fatal("template save remained busy")
	}
	templates, err := store.ListTemplates()
	if err != nil || len(templates) != 1 {
		t.Fatalf("templates = %#v, %v", templates, err)
	}

	h.app.beginNewAgentTask()
	h.app.agents.templateChoice = "Go isolated service"
	h.app.applySelectedAgentTemplate()
	if h.app.agents.runCommand != "run --database {database}" || h.app.agents.developmentPort != "4310" ||
		h.app.agents.environment != "FEATURE_FLAG=enabled\nLOG_LEVEL=debug" ||
		h.app.agents.ignoredFiles != ".env\n.cache/config.json" {
		t.Fatalf("applied form does not contain template: %#v", h.app.agents)
	}

	h.app.confirmDeleteAgentTemplate()
	if h.app.confirm == nil || !h.app.confirm.destructive {
		t.Fatal("template deletion did not require destructive confirmation")
	}
	if templates, _ := store.ListTemplates(); len(templates) != 1 {
		t.Fatal("template was deleted before confirmation")
	}
	h.app.acceptConfirm()
	if templates, _ := store.ListTemplates(); len(templates) != 0 {
		t.Fatalf("template survived confirmed deletion: %#v", templates)
	}
}

func TestAgentTaskActionsUseCompactIconButtons(t *testing.T) {
	h, store := newAgentTestHarness(t)
	createAgentTestTask(t, h, store, agentwork.Task{
		Status:  agentwork.StatusExited,
		Profile: agentwork.Profile{LaunchCommand: "agent", TestCommand: "test", RunCommand: "run"},
	})
	h.tester.Frame()
	for _, label := range []string{"Refresh tasks", "Analyze overlaps", "Integration dry run", "Coordinated updates", "Batch tests and reviews", "Resume agent", "Open worktree", "Review changes", "Run tests", "Run profile", "Mark ready for review", "Mark done", "Archive task"} {
		bounds, found := h.tester.Find(label)
		if !found {
			t.Fatalf("Agent icon action %q is missing from %q", label, h.tester.Texts())
		}
		if bounds.W > 40 || bounds.H > 30 {
			t.Fatalf("Agent action %q is not a compact icon button: bounds=%+v", label, bounds)
		}
	}
}

func TestAgentEnvironmentRejectsImplicitIdentityOverrides(t *testing.T) {
	for _, value := range []string{"MISSING_EQUALS", "1PORT=4310", "TANGIT_AGENT_TASK_ID=other"} {
		if _, err := parseAgentEnvironment(value); err == nil {
			t.Fatalf("environment %q was accepted", value)
		}
	}
	environment, err := parseAgentEnvironment("PORT=4310\nMESSAGE=hello=world")
	if err != nil || environment["MESSAGE"] != "hello=world" {
		t.Fatalf("explicit environment = %#v, %v", environment, err)
	}
}
