package app

import (
	"fmt"
	"log"
	"time"

	"github.com/egoist/mygo"

	"lazygitapp/internal/agentwork"
)

// agentTaskAttention is one durable result that may need the user's attention.
// Its ID identifies this particular task update to both notification surfaces.
type agentTaskAttention struct {
	id          string
	kind        string
	title       string
	description string
}

// agentTaskAttentions compares durable task values. Initial list loads and
// unrelated updates do not produce attention; only explicit transitions do.
func agentTaskAttentions(previous, updated agentwork.Task) []agentTaskAttention {
	if previous.ID == "" || previous.ID != updated.ID {
		return nil
	}
	attentions := make([]agentTaskAttention, 0, 2)
	appendAttention := func(result, kind, title, description string) {
		attentions = append(attentions, agentTaskAttention{
			id:          fmt.Sprintf("agent-task-%s-%d-%s", updated.ID, updated.UpdatedAt.UTC().UnixNano(), result),
			kind:        kind,
			title:       title,
			description: description,
		})
	}
	if previous.Status != updated.Status {
		switch updated.Status {
		case agentwork.StatusNeedsInput:
			appendAttention("status-needs-input", "warning", "Agent needs input",
				trf("Agent task %s is waiting for your response.", updated.Title))
		case agentwork.StatusReadyForReview:
			appendAttention("status-ready-for-review", "success", "Agent ready for review",
				trf("Agent task %s is ready for your review.", updated.Title))
		case agentwork.StatusExited:
			appendAttention("status-exited", "", "Agent exited",
				trf("Agent task %s exited. Review readiness is NOT inferred; inspect its work before marking it ready for review.", updated.Title))
		case agentwork.StatusFailed:
			appendAttention("status-failed", "error", "Agent task failed",
				trf("Agent task %s failed. Inspect its terminal output and worktree.", updated.Title))
		}
	}

	previousTest, updatedTest := agentwork.TestStatus(""), agentwork.TestStatus("")
	if previous.LastTest != nil {
		previousTest = previous.LastTest.Status
	}
	if updated.LastTest != nil {
		updatedTest = updated.LastTest.Status
	}
	if previousTest != updatedTest {
		switch updatedTest {
		case agentwork.TestStatusPassed:
			appendAttention("test-passed", "success", "Agent tests passed",
				trf("The latest tests passed for %s.", updated.Title))
		case agentwork.TestStatusFailed:
			appendAttention("test-failed", "error", "Agent tests failed",
				trf("The latest tests failed for %s. Inspect the terminal output.", updated.Title))
		}
	}
	return attentions
}

// deliverAgentDesktopNotification is a no-op until Main installs native
// delivery. Tests replace it with a stub and never enter MyGo's native backend.
var deliverAgentDesktopNotification = func(agentTaskAttention) {}

func (a *app) notifyAgentAttentions(attentions []agentTaskAttention) {
	for _, attention := range attentions {
		attention.title = tr(attention.title)
		a.notifyToast(appToast{
			id:          attention.id,
			kind:        attention.kind,
			title:       attention.title,
			description: attention.description,
			timeout:     8 * time.Second,
		})
		deliverAgentDesktopNotification(attention)
	}
}

// showAgentDesktopNotification keeps native delivery and authorization waits
// off the UI thread. Delivery failures stay in the log rather than recursively
// creating another notification about the failed notification.
func showAgentDesktopNotification(attention agentTaskAttention) {
	go func() {
		notification := mygo.NewNotification(mygo.NotificationOptions{
			ID:    attention.id,
			Title: attention.title,
			Body:  attention.description,
		})
		if err := notification.Show(); err != nil {
			log.Printf("Could not show agent task desktop notification: %v", err)
		}
	}()
}
