package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// Chinese Reflog uses Git terminology rather than literal reference "movements".
// Keep the displayed copy aligned with the actual records the query searches.
func TestReflogChineseLabelsAndSearch(t *testing.T) {
	originalLanguage := currentLanguage
	t.Cleanup(func() { currentLanguage = originalLanguage })
	for _, test := range []struct {
		code, title, placeholder string
	}{
		{code: "zh-Hans", title: "引用日志", placeholder: "搜索提交哈希、提交说明或作者"},
		{code: "zh-Hant", title: "參照日誌", placeholder: "搜尋提交雜湊、提交說明或作者"},
	} {
		t.Run(test.code, func(t *testing.T) {
			currentLanguage = test.code
			if got := tr("Recent reference movements"); got != test.title {
				t.Errorf("localized Reflog title = %q, want %q", got, test.title)
			}
			if got := tr("Filter by operation, hash, or author"); got != test.placeholder {
				t.Errorf("localized Reflog placeholder = %q, want %q", got, test.placeholder)
			}
			var handle ui.Handle
			query := ""
			// Exercise the actual Chinese input binding too, not just the catalog.
			ui.NewTester(func(c *ui.Context) {
				listSearchField(c, &handle, &query, "Search reflog", "Filter by operation, hash, or author", "Clear reflog search", 20, 20, func() {})
			}, 340, 120)
		})
	}

	currentLanguage = "zh-Hans"
	a := newApp(fakeOpener{})
	a.snapshot.Reflog = []repository.Commit{
		{Hash: "abcdef01", ShortHash: "abcdef0", Subject: "解决分支冲突", AuthorName: "张三", AuthorEmail: "zhang@example.com"},
		{Hash: "12345678", ShortHash: "1234567", Subject: "merge: feature", AuthorName: "Ada", AuthorEmail: "ada@example.com"},
	}
	for _, test := range []struct {
		name, query string
		want        []int
	}{
		{name: "Chinese commit message", query: "冲突", want: []int{0}},
		{name: "Chinese author", query: "张三", want: []int{0}},
		{name: "author email", query: "zhang@example.com", want: []int{0}},
		{name: "short hash", query: "1234567", want: []int{1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			a.reflogFilter = test.query
			if got := a.visibleReflog(); !slices.Equal(got, test.want) {
				t.Fatalf("reflog search for %q returned %v, want %v", test.query, got, test.want)
			}
		})
	}
}
