package app

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"lazygitapp/internal/agentwork"
)

func TestAgentTaskAttentionsFilterDurableTransitions(t *testing.T) {
	updatedAt := time.Date(2026, time.November, 13, 12, 0, 0, 123, time.UTC)
	base := agentwork.Task{
		ID:        "0123456789abcdef0123456789abcdef",
		Title:     "Implement repository search",
		Status:    agentwork.StatusRunning,
		UpdatedAt: updatedAt.Add(-time.Minute),
		LastTest: &agentwork.TestResult{
			Status:    agentwork.TestStatusRunning,
			StartedAt: updatedAt.Add(-time.Minute),
		},
	}

	for _, test := range []struct {
		name       string
		update     func(*agentwork.Task)
		wantResult string
		wantKind   string
		wantTitle  string
	}{
		{
			name: "needs input",
			update: func(task *agentwork.Task) {
				task.Status = agentwork.StatusNeedsInput
			},
			wantResult: "status-needs-input", wantKind: "warning", wantTitle: "Agent needs input",
		},
		{
			name: "ready for review",
			update: func(task *agentwork.Task) {
				task.Status = agentwork.StatusReadyForReview
			},
			wantResult: "status-ready-for-review", wantKind: "success", wantTitle: "Agent ready for review",
		},
		{
			name: "exited",
			update: func(task *agentwork.Task) {
				task.Status = agentwork.StatusExited
			},
			wantResult: "status-exited", wantTitle: "Agent exited",
		},
		{
			name: "failed",
			update: func(task *agentwork.Task) {
				task.Status = agentwork.StatusFailed
			},
			wantResult: "status-failed", wantKind: "error", wantTitle: "Agent task failed",
		},
		{
			name: "tests passed",
			update: func(task *agentwork.Task) {
				task.LastTest.Status = agentwork.TestStatusPassed
			},
			wantResult: "test-passed", wantKind: "success", wantTitle: "Agent tests passed",
		},
		{
			name: "tests failed",
			update: func(task *agentwork.Task) {
				task.LastTest.Status = agentwork.TestStatusFailed
			},
			wantResult: "test-failed", wantKind: "error", wantTitle: "Agent tests failed",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			previous := base.Clone()
			updated := base.Clone()
			updated.UpdatedAt = updatedAt
			test.update(&updated)

			attentions := agentTaskAttentions(previous, updated)
			if len(attentions) != 1 {
				t.Fatalf("attentions = %#v, want exactly one", attentions)
			}
			attention := attentions[0]
			if attention.kind != test.wantKind || attention.title != test.wantTitle || attention.description == "" {
				t.Fatalf("attention = %#v, want kind %q and title %q with a description", attention, test.wantKind, test.wantTitle)
			}
			wantID := "agent-task-" + base.ID + "-" + strconv.FormatInt(updatedAt.UnixNano(), 10) + "-" + test.wantResult
			if attention.id != wantID {
				t.Fatalf("attention ID = %q, want stable update/result ID %q", attention.id, wantID)
			}
			if again := agentTaskAttentions(previous, updated); len(again) != 1 || again[0] != attention {
				t.Fatalf("same durable transition produced a different attention: first=%#v again=%#v", attention, again)
			}
			if updated.Status == agentwork.StatusExited {
				if updated.Status == agentwork.StatusReadyForReview {
					t.Fatal("exiting inferred review readiness")
				}
				if !strings.Contains(attention.description, "Review readiness is NOT inferred") {
					t.Fatalf("exit description does not reject readiness inference: %q", attention.description)
				}
			}
		})
	}
}

func TestAgentTaskAttentionsIgnoreNonTransitionsAndDuplicates(t *testing.T) {
	updatedAt := time.Date(2026, time.November, 13, 12, 0, 0, 0, time.UTC)
	passedAt := updatedAt.Add(-time.Minute)
	base := agentwork.Task{
		ID:        "0123456789abcdef0123456789abcdef",
		Title:     "Implement repository search",
		Status:    agentwork.StatusNeedsInput,
		UpdatedAt: passedAt,
		LastTest: &agentwork.TestResult{
			Status: agentwork.TestStatusPassed, StartedAt: passedAt.Add(-time.Second), FinishedAt: passedAt,
		},
	}

	for _, test := range []struct {
		name     string
		previous agentwork.Task
		update   func(*agentwork.Task)
	}{
		{
			name: "initial task load has no durable previous value",
			update: func(task *agentwork.Task) {
				task.Status = agentwork.StatusNeedsInput
			},
		},
		{
			name:     "duplicate needs input status",
			previous: base,
			update:   func(task *agentwork.Task) {},
		},
		{
			name: "launch and running status",
			previous: func() agentwork.Task {
				task := base.Clone()
				task.Status = agentwork.StatusReady
				return task
			}(),
			update: func(task *agentwork.Task) {
				task.Status = agentwork.StatusRunning
			},
		},
		{
			name:     "checkpoint only update",
			previous: base,
			update: func(task *agentwork.Task) {
				task.Checkpoints = append(task.Checkpoints, agentwork.Checkpoint{ID: "checkpoint"})
			},
		},
		{
			name:     "running test result",
			previous: base,
			update: func(task *agentwork.Task) {
				task.LastTest.Status = agentwork.TestStatusRunning
			},
		},
		{
			name:     "duplicate same test result",
			previous: base,
			update: func(task *agentwork.Task) {
				task.LastTest.FinishedAt = updatedAt
			},
		},
		{
			name:     "non attention status",
			previous: base,
			update: func(task *agentwork.Task) {
				task.Status = agentwork.StatusDone
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			previous := test.previous.Clone()
			updated := base.Clone()
			updated.UpdatedAt = updatedAt
			test.update(&updated)
			if attentions := agentTaskAttentions(previous, updated); len(attentions) != 0 {
				t.Fatalf("non-transition produced attentions: %#v", attentions)
			}
		})
	}
}

func TestAgentTaskAttentionsIncludeStatusAndTestResult(t *testing.T) {
	previous := agentwork.Task{
		ID: "0123456789abcdef0123456789abcdef", Title: "Task", Status: agentwork.StatusRunning,
		LastTest: &agentwork.TestResult{Status: agentwork.TestStatusRunning},
	}
	updated := previous.Clone()
	updated.Status = agentwork.StatusReadyForReview
	updated.LastTest.Status = agentwork.TestStatusFailed
	updated.UpdatedAt = time.Now()

	attentions := agentTaskAttentions(previous, updated)
	if len(attentions) != 2 || attentions[0].title != "Agent ready for review" || attentions[1].title != "Agent tests failed" {
		t.Fatalf("combined transition attentions = %#v", attentions)
	}
	if attentions[0].id == attentions[1].id {
		t.Fatalf("different results share notification ID %q", attentions[0].id)
	}
}

func TestAgentMutationDeliversNeedsInputToastAndDesktopNotification(t *testing.T) {
	delivered := make(chan agentTaskAttention, 2)
	previousDelivery := deliverAgentDesktopNotification
	deliverAgentDesktopNotification = func(attention agentTaskAttention) { delivered <- attention }
	t.Cleanup(func() { deliverAgentDesktopNotification = previousDelivery })

	h, store := newAgentTestHarness(t)
	task := createAgentTestTask(t, h, store, agentwork.Task{Status: agentwork.StatusRunning})
	h.app.mutateAgentTask(task.ID, func(current *agentwork.Task) error {
		current.Status = agentwork.StatusNeedsInput
		return nil
	})
	h.waitFor(t, "needs-input task mutation", func() bool {
		current, ok := h.app.selectedAgentTask()
		return ok && current.Status == agentwork.StatusNeedsInput && !h.app.agents.mutating[task.ID]
	})

	var attention agentTaskAttention
	select {
	case attention = <-delivered:
	default:
		t.Fatal("task mutation did not deliver a desktop notification")
	}
	if attention.title != "Agent needs input" || !strings.Contains(attention.description, task.Title) {
		t.Fatalf("desktop attention = %#v", attention)
	}
	current, _ := h.app.selectedAgentTask()
	wantID := "agent-task-" + task.ID + "-" + strconv.FormatInt(current.UpdatedAt.UnixNano(), 10) + "-status-needs-input"
	if attention.id != wantID {
		t.Fatalf("desktop notification ID = %q, want %q", attention.id, wantID)
	}
	if !h.tester.HasText(attention.title) || !h.tester.HasText(attention.description) {
		t.Fatalf("matching toast not rendered for %#v; texts=%q", attention, h.tester.Texts())
	}
	select {
	case duplicate := <-delivered:
		t.Fatalf("one transition delivered duplicate desktop notifications: %#v", duplicate)
	default:
	}
}
