package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/repository"
)

// A compact operation card must spend its horizontal space on the operation
// and conflict guidance, not on secondary completion metadata or a long ref.
func TestOperationContextRespondsToItsPaneWidth(t *testing.T) {
	for _, test := range []struct {
		name              string
		width             int
		wantDone          bool
		wantQualifyingRef string
	}{
		{name: "narrow uses compact context", width: 300, wantQualifyingRef: "demo/rebase-topic"},
		{name: "wide keeps all metadata", width: 560, wantDone: true, wantQualifyingRef: "refs/heads/demo/rebase-topic"},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := newApp(fakeOpener{})
			a.snapshot.OperationState = &repository.OperationState{
				Type: repository.OperationTypeRebase, Step: 3, Done: 2, Total: 5,
				FullRefs: []string{"refs/heads/demo/rebase-topic"},
			}
			a.snapshot.Files = []repository.File{
				{Path: "one.txt", Status: "UU", HasMergeConflicts: true},
				{Path: "two.txt", Status: "AA", HasMergeConflicts: true},
			}
			tt := ui.NewTester(func(c *ui.Context) {
				a.operationContextView(c)
			}, test.width, 220)
			tt.SetDark(false)
			tt.Frame()

			count, found := tt.Find("2 conflicts to resolve")
			if !found {
				t.Fatalf("the operation's conflict headline is missing: %q", tt.Texts())
			}
			hint, found := tt.Find("Pick a side, then Continue.")
			if !found {
				t.Fatalf("the operation's conflict guidance is missing: %q", tt.Texts())
			}
			progress, found := tt.Find("Step 3 of 5")
			if !found {
				t.Fatal("the operation progress is missing")
			}
			if hint.Y < count.Y+count.H {
				t.Errorf("conflict guidance starts at %.1f, before the headline at %.1f ends at %.1f; it needs its own line", hint.Y, count.Y, count.Y+count.H)
			}
			if hint.H < progress.H {
				t.Errorf("conflict guidance height %.1f is smaller than normal body text", hint.H)
			}
			if slices.Contains(tt.Texts(), "2 completed") != test.wantDone {
				t.Errorf("completed metadata presence = %v, want %v", !test.wantDone, test.wantDone)
			}
			if !slices.Contains(tt.Texts(), test.wantQualifyingRef) {
				t.Errorf("context does not contain %q: %q", test.wantQualifyingRef, tt.Texts())
			}
		})
	}
}
