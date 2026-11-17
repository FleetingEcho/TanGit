package app

import (
	"slices"
	"testing"

	"lazygitapp/internal/repository"
)

// The Chinese placeholder describes precisely what visibleWorktrees searches:
// path / name, checked-out branch and the commit behind a linked worktree.
func TestWorktreeChineseLabelsAndSearch(t *testing.T) {
	originalLanguage := currentLanguage
	t.Cleanup(func() { currentLanguage = originalLanguage })
	for _, test := range []struct {
		code, title, placeholder string
	}{
		{code: "zh-Hans", title: "工作树", placeholder: "搜索名称、路径、分支或提交"},
		{code: "zh-Hant", title: "工作樹", placeholder: "搜尋名稱、路徑、分支或提交"},
	} {
		t.Run(test.code, func(t *testing.T) {
			currentLanguage = test.code
			if got := tr("Linked worktrees"); got != test.title {
				t.Errorf("localized worktree title = %q, want %q", got, test.title)
			}
			if got := tr("Filter by name, path, branch, or commit"); got != test.placeholder {
				t.Errorf("localized worktree placeholder = %q, want %q", got, test.placeholder)
			}
		})
	}

	currentLanguage = "zh-Hans"
	a := newApp(fakeOpener{})
	a.snapshot.Worktrees = []repository.Worktree{
		{Name: "feature-tree", Path: "/work/feature", Branch: "feature/auth", Head: "efcd567890ef"},
		{Name: "keep-notes", Path: "/work/notes", Branch: "notes", Head: "123456789abc"},
	}
	for _, test := range []struct {
		name, query string
		want        []int
	}{
		{name: "name", query: "feature-tree", want: []int{0}},
		{name: "path", query: "/work/feature", want: []int{0}},
		{name: "branch", query: "auth", want: []int{0}},
		{name: "head hash", query: "efcd5678", want: []int{0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			a.worktreeFilter = test.query
			if got := a.visibleWorktrees(); !slices.Equal(got, test.want) {
				t.Fatalf("worktree search for %q returned %v, want %v", test.query, got, test.want)
			}
		})
	}
}
