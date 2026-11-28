package app

import (
	"testing"

	"github.com/FleetingEcho/lazygitGo/internal/forge"
)

func TestRequestBranchURLPointsAForkBranchAtTheFork(t *testing.T) {
	item := forge.ChangeRequest{
		Provider: forge.GitHub, Number: 42, URL: "https://github.example.com/group/project/pull/42",
		SourceBranch: "patch-1", TargetBranch: "master",
		CrossRepo: true, SourceRepoURL: "https://github.example.com/alice/project",
	}
	if got, want := requestBranchURL(item, "patch-1", true), "https://github.example.com/alice/project/tree/patch-1"; got != want {
		t.Fatalf("source = %q, want %q", got, want)
	}
	if got, want := requestBranchURL(item, "master", false), "https://github.example.com/group/project/tree/master"; got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
	// A fork the provider did not name must not fall back to the base repository.
	item.SourceRepoURL = ""
	if got := requestBranchURL(item, "patch-1", true); got != "" {
		t.Fatalf("unknown fork linked to %q", got)
	}
	same := item
	same.CrossRepo = false
	if got, want := requestBranchURL(same, "topic", true), "https://github.example.com/group/project/tree/topic"; got != want {
		t.Fatalf("same-repo source = %q, want %q", got, want)
	}
	if requestCheckoutCommand(item) != "gh pr checkout 42" || requestNumberText(item) != "#42" {
		t.Fatalf("copy texts: %q %q", requestCheckoutCommand(item), requestNumberText(item))
	}
	gl := forge.ChangeRequest{Provider: forge.GitLab, Number: 7}
	if requestCheckoutCommand(gl) != "glab mr checkout 7" || requestNumberText(gl) != "!7" {
		t.Fatalf("gitlab copy texts: %q %q", requestCheckoutCommand(gl), requestNumberText(gl))
	}
}

func TestRequestListRowHelpers(t *testing.T) {
	draft := forge.ChangeRequest{Draft: true, Title: "Draft: Refactor parser"}
	if got := requestListTitle(draft); got != "Refactor parser" {
		t.Fatalf("draft title = %q", got)
	}
	for title, want := range map[string]string{"DRAFT: x": "x", "[Draft] y": "y", "Drafting tools": "Drafting tools", "Draft:": "Draft:"} {
		if got := requestListTitle(forge.ChangeRequest{Draft: true, Title: title}); got != want {
			t.Fatalf("title %q = %q, want %q", title, got, want)
		}
	}
	if got := requestListTitle(forge.ChangeRequest{Title: "Draft: keeps it"}); got != "Draft: keeps it" {
		t.Fatalf("a non-draft keeps its title, got %q", got)
	}
	long := "feature/a-very-long-source-branch-name"
	short := middleEllipsis(long, 26)
	if len([]rune(short)) != 26 || short[:12] != long[:12] || short[len(short)-5:] != long[len(long)-5:] {
		t.Fatalf("ellipsis = %q", short)
	}
	if middleEllipsis("main", 26) != "main" || middleEllipsis("héllo-wörld-日本語-branch-name-long", 10) == "" {
		t.Fatal("short and non-ASCII names must be handled")
	}
	if !isDefaultBranchName("main") || !isDefaultBranchName("master") || isDefaultBranchName("7.6") {
		t.Fatal("default branch names")
	}
}
