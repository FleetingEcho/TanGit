package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/forge"
)

func changeRequestFilterDialogBounds(t *testing.T, tester *ui.Tester) ui.Rect {
	t.Helper()
	for _, item := range tester.AccessAll() {
		if item.Role == ui.RoleDialog && item.Name == "Filter pull requests" {
			return item.Bounds
		}
	}
	t.Fatalf("filter dialog is missing: %q", tester.Texts())
	return ui.Rect{}
}

func TestChangeRequestFilterDialogHeightDoesNotJumpWhenFacetsLoad(t *testing.T) {
	a := newApp(fakeOpener{})
	a.changeRequests.filterOpen = true
	a.changeRequests.facetsLoading = true
	tester := ui.NewTester(a.changeRequestFilterModal, 900, 760)
	tester.Frame()
	loading := changeRequestFilterDialogBounds(t, tester)

	a.changeRequests.facetsLoading = false
	a.changeRequests.facetsLoaded = true
	a.changeRequests.facets = forge.Facets{
		People: []string{"alice", "bob"}, Labels: []string{"bug", "feature"},
	}
	tester.Frame()
	loaded := changeRequestFilterDialogBounds(t, tester)
	if loading.H != loaded.H {
		t.Fatalf("filter dialog height changed after facets loaded: loading=%+v loaded=%+v", loading, loaded)
	}
}

func TestGitLabChangeRequestFilterOffersAllSupportedSortFields(t *testing.T) {
	choices := changeRequestSortChoices(forge.GitLab)
	want := []forge.SortField{
		forge.SortCreated, forge.SortUpdated, forge.SortMerged, forge.SortMilestoneDue,
		forge.SortPopularity, forge.SortLabelPriority, forge.SortPriority, forge.SortTitle,
	}
	if len(choices) != len(want) {
		t.Fatalf("GitLab sort choices = %#v", choices)
	}
	for i, field := range want {
		if choices[i].field != field {
			t.Fatalf("GitLab sort %d = %q, want %q", i, choices[i].field, field)
		}
	}
}

func TestChangeRequestQueryDefaultsToNewestUpdated(t *testing.T) {
	a := newApp(fakeOpener{})
	query := a.changeRequestQuery()
	if query.OrderBy != forge.SortUpdated || query.Sort != forge.SortDescending {
		t.Fatalf("default sort = %q %q", query.OrderBy, query.Sort)
	}
}
