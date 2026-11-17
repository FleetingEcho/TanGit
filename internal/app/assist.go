package app

import (
	"errors"
	"fmt"
	"strings"

	"lazygitapp/internal/agentassist"
)

var errAssistNotConfigured = errors.New("no AssistCommand is configured in the current Agent Profile")

// assistUIState owns provider-neutral suggestion jobs. The advisor and factory
// are test/fake seams; production constructs the existing JSON-file
// CommandAdvisor only inside an explicit Get suggestions action.
type assistUIState struct {
	advisor        agentassist.Advisor
	advisorFactory func(string) (agentassist.Advisor, error)
	conflict       conflictAssistUIState
	splitter       changeSplitUIState
}

func newAssistAdvisor(injected agentassist.Advisor, factory func(string) (agentassist.Advisor, error), command string) (agentassist.Advisor, string, error) {
	if injected != nil {
		return injected, "Injected preview advisor", nil
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return nil, "Not configured", errAssistNotConfigured
	}
	if factory != nil {
		advisor, err := factory(command)
		if err == nil && advisor == nil {
			err = fmt.Errorf("AssistCommand advisor factory returned nil")
		}
		return advisor, "External Agent Profile helper", err
	}
	advisor, err := agentassist.NewCommandAdvisor(agentassist.CommandConfig{Template: command})
	if err == nil && advisor == nil {
		err = fmt.Errorf("AssistCommand advisor is nil")
	}
	return advisor, "External Agent Profile helper", err
}

func (a *app) advisorForAssistRequest() (agentassist.Advisor, string, error) {
	return newAssistAdvisor(a.assistance.advisor, a.assistance.advisorFactory, a.agents.assistCommand)
}

func (a *app) assistCommandConfigured() bool {
	return a.assistance.advisor != nil || strings.TrimSpace(a.agents.assistCommand) != ""
}

func (a *app) assistSourceBeforeRequest() string {
	if a.assistance.advisor != nil {
		return "Injected preview advisor"
	}
	if strings.TrimSpace(a.agents.assistCommand) != "" {
		return "External Agent Profile helper"
	}
	return "Not configured"
}

// cancelAssistance retires both suggestion jobs without applying or accepting
// anything. Repository replacement uses this in addition to each close button.
func (a *app) cancelAssistance() {
	a.closeConflictAssistant()
	a.closeChangeSplitter()
}
