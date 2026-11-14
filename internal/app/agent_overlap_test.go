package app

import (
	"context"
	"errors"
	"testing"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
)

type agentOverlapTarget struct {
	fakeSession
	files   []repository.CommitFile
	patches map[string]repository.Diff
	options []repository.DiffOptions
}

func (s *agentOverlapTarget) FinalFiles(_ context.Context, _ string, options repository.DiffOptions) ([]repository.CommitFile, error) {
	s.options = append(s.options, options)
	return append([]repository.CommitFile(nil), s.files...), nil
}

func (s *agentOverlapTarget) FinalFileDiff(_ context.Context, _ string, file repository.CommitFile, options repository.DiffOptions) (repository.Diff, error) {
	s.options = append(s.options, options)
	return s.patches[file.Path], nil
}

type agentOverlapOpener struct {
	targets map[string]*agentOverlapTarget
}

func (o agentOverlapOpener) Open(_ context.Context, path string) (repository.Session, error) {
	if target := o.targets[path]; target != nil {
		return target, nil
	}
	return nil, errors.New("unknown explicit target " + path)
}
func (o agentOverlapOpener) Init(context.Context, string) error { return nil }

func TestAgentOverlapUsesDirectZeroContextFinalResults(t *testing.T) {
	h, _ := newAgentTestHarness(t)
	left := agentwork.Task{ID: "11111111111111111111111111111111", Title: "Left", BaseCommit: "same-base", WorktreePath: "/work/left"}
	right := agentwork.Task{ID: "22222222222222222222222222222222", Title: "Right", BaseCommit: "same-base", WorktreePath: "/work/right"}
	patch := "@@ -10,2 +10,2 @@\n-old\n+new\n"
	leftTarget := &agentOverlapTarget{files: []repository.CommitFile{{Path: "shared.go", Status: "M"}}, patches: map[string]repository.Diff{"shared.go": {Patch: patch}}}
	rightTarget := &agentOverlapTarget{files: []repository.CommitFile{{Path: "shared.go", Status: "M"}}, patches: map[string]repository.Diff{"shared.go": {Patch: patch}}}
	h.app.opener = agentOverlapOpener{targets: map[string]*agentOverlapTarget{left.WorktreePath: leftTarget, right.WorktreePath: rightTarget}}
	h.app.agents.tasks = []agentwork.Task{left, right}

	h.app.beginAgentOverlapAnalysis()
	h.waitFor(t, "overlap analysis", func() bool { return !h.app.agents.overlap.loading })
	if len(h.app.agents.overlap.errors) != 0 {
		t.Fatalf("overlap failures: %#v", h.app.agents.overlap.errors)
	}
	report := h.app.agents.overlap.report
	if len(report.Findings) != 2 || report.Findings[0].Kind != agentwork.OverlapHunkExact || report.Findings[1].Kind != agentwork.OverlapSamePath {
		t.Fatalf("overlap findings = %#v", report.Findings)
	}
	for _, finding := range report.Findings {
		if !finding.BaseComparable {
			t.Fatalf("same-base finding is not comparable: %#v", finding)
		}
	}
	for _, target := range []*agentOverlapTarget{leftTarget, rightTarget} {
		if len(target.options) != 2 || target.options[0].ContextLines != 0 || target.options[1].ContextLines != 0 {
			t.Fatalf("analysis ignored zero-context options: %#v", target.options)
		}
	}
}

func TestAgentOverlapReportsPartialTargetFailures(t *testing.T) {
	h, _ := newAgentTestHarness(t)
	h.app.agents.tasks = []agentwork.Task{
		{ID: "11111111111111111111111111111111", Title: "Readable", BaseCommit: "base", WorktreePath: "/work/readable"},
		{ID: "22222222222222222222222222222222", Title: "Missing", BaseCommit: "base", WorktreePath: "/work/missing"},
	}
	h.app.opener = agentOverlapOpener{targets: map[string]*agentOverlapTarget{"/work/readable": {}}}
	h.app.beginAgentOverlapAnalysis()
	h.waitFor(t, "partial overlap analysis", func() bool { return !h.app.agents.overlap.loading })
	if len(h.app.agents.overlap.errors) != 1 || len(h.app.agents.overlap.report.Findings) != 0 {
		t.Fatalf("partial result = errors %#v findings %#v", h.app.agents.overlap.errors, h.app.agents.overlap.report.Findings)
	}
	h.tester.Frame()
	if !h.tester.HasText("Some tasks could not be analyzed; findings below are partial.") {
		t.Fatalf("partial warning is not visible: %q", h.tester.Texts())
	}
}
