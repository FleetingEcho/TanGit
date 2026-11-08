package app

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"lazygitapp/internal/agentwork"
)

func TestAgentTaskEnvironmentIsolatesResourcesAndProtectsIdentity(t *testing.T) {
	task := agentwork.Task{
		ID: "0123456789abcdef0123456789abcdef", WorktreePath: filepath.Join(string(filepath.Separator), "work", "agent"),
		Profile: agentwork.Profile{
			Environment: map[string]string{
				"LOG_LEVEL": "debug", "TANGIT_AGENT_TASK_ID": "wrong-target", "IGNORED=NAME": "ignored",
			},
			Resources: agentwork.Resources{DevelopmentPort: 4310, DatabaseName: "task_search", CacheDirectory: ".cache/agent"},
		},
	}
	requestPath := filepath.Join(string(filepath.Separator), "config", "request.md")
	operationID := agentTestOperationID(task.ID)
	environment := agentTaskEnvironment(task, requestPath, operationID)
	for _, expected := range []string{
		"LOG_LEVEL=debug", "TANGIT_AGENT_TASK_ID=" + task.ID, "TANGIT_AGENT_OPERATION_ID=" + operationID,
		"TANGIT_AGENT_WORKTREE=" + task.WorktreePath, "TANGIT_AGENT_REQUEST_FILE=" + requestPath,
		"TANGIT_AGENT_PORT=4310", "TANGIT_AGENT_DATABASE=task_search",
		"TANGIT_AGENT_CACHE_DIR=" + filepath.Join(task.WorktreePath, ".cache/agent"),
	} {
		if !slices.Contains(environment, expected) {
			t.Errorf("environment %#v does not contain %q", environment, expected)
		}
	}
	for _, entry := range environment {
		if strings.HasPrefix(entry, "IGNORED=") || entry == "TANGIT_AGENT_TASK_ID=wrong-target" {
			t.Errorf("unsafe identity/environment entry survived: %q", entry)
		}
	}
	if !slices.IsSorted(environment) {
		t.Errorf("environment is not deterministic: %#v", environment)
	}
}

func TestAgentRunTerminalDoesNotChangeLifecycle(t *testing.T) {
	h, store := newAgentTestHarness(t)
	task := createAgentTestTask(t, h, store, agentwork.Task{Status: agentwork.StatusReadyForReview})
	h.app.agentTerminalExited(agentRunOperationID(task.ID), 0)
	current, err := store.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != agentwork.StatusReadyForReview || len(current.Activity) != 0 {
		t.Fatalf("run exit mutated agent lifecycle/history: %#v", current)
	}
}
