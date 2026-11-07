package app

import (
	"strings"
	"testing"

	"lazygitapp/internal/repository"
)

func TestIgnoreSelectedFileRoutesOnlyUntrackedUnstagedFiles(t *testing.T) {
	tests := []struct {
		name  string
		file  repository.File
		local bool
		want  string
	}{
		{
			name: "untracked file uses gitignore",
			file: repository.File{Path: "scratch.txt", Status: "??", Unstaged: true, Added: true},
			want: "ignore:scratch.txt",
		},
		{
			name:  "local ignore uses exclude",
			file:  repository.File{Path: "notes.txt", Status: "??", Unstaged: true, Added: true},
			local: true,
			want:  "exclude:notes.txt",
		},
		{
			name: "tracked unstaged file is ineligible",
			file: repository.File{Path: "tracked.txt", Status: " M", Unstaged: true, Tracked: true},
		},
		{
			name: "staged file is ineligible even without tracked flag",
			file: repository.File{Path: "staged.txt", Status: "A ", Staged: true, Added: true},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h, calls := changesTestApp(t, []repository.File{test.file})
			selected := changeRowIndex(h.app.changeRows, test.file.Path, test.file.Staged)
			if selected < 0 {
				t.Fatalf("fixture has no selected row for %#v", test.file)
			}
			h.app.selectedFile = selected

			h.app.ignoreSelectedFile(test.local)

			if test.want == "" {
				if h.app.actionBusy {
					t.Fatal("an ineligible file started a repository action")
				}
				for _, call := range calls.all() {
					if strings.HasPrefix(call, "ignore:") || strings.HasPrefix(call, "exclude:") {
						t.Fatalf("an ineligible file caused %q", call)
					}
				}
				return
			}

			h.waitFor(t, "the ignore operation", func() bool { return calls.has(test.want) })
			other := "exclude:" + test.file.Path
			if test.local {
				other = "ignore:" + test.file.Path
			}
			if test.want == other {
				t.Fatalf("test fixture does not distinguish global and local routing")
			}
			if calls.has(other) {
				t.Fatalf("ignore used the wrong destination: %q", calls.all())
			}
		})
	}
}

func TestRestoreSelectedHistoricalFileWaitsForConfirmationAndUsesSelection(t *testing.T) {
	h, calls := historyHarness(t)
	h.waitFor(t, "the historical files", func() bool { return len(h.app.commitFiles) == 2 })

	// Choose the root commit and its second file directly, so a failure cannot
	// accidentally pass by restoring the fixture's default commit or file.
	h.app.selectedCommit = 1
	h.app.selectedCommitFile = 1
	commit := h.app.snapshot.Commits[1]
	file := h.app.commitFiles[1]
	wantCall := "restore:" + commit.Hash + ":" + file.Path

	h.app.confirmRestoreSelectedHistoricalFile()
	if h.app.confirm == nil {
		t.Fatal("historical restore did not ask for confirmation")
	}
	if !h.app.confirm.destructive || h.app.confirm.confirmLabel != "Restore file" {
		t.Fatalf("unexpected confirmation request: %#v", h.app.confirm)
	}
	if !strings.Contains(h.app.confirm.message, file.Path) || !strings.Contains(h.app.confirm.message, commit.ShortHash) {
		t.Fatalf("confirmation does not identify the selected commit and file: %q", h.app.confirm.message)
	}
	for _, call := range calls.all() {
		if strings.HasPrefix(call, "restore:") {
			t.Fatalf("historical restore ran before confirmation: %q", calls.all())
		}
	}

	h.tester.Frame()
	if err := h.tester.Click("Restore file"); err != nil {
		t.Fatalf("confirm historical restore: %v", err)
	}
	h.waitFor(t, "the selected historical restore", func() bool { return calls.has(wantCall) })
	for _, call := range calls.all() {
		if strings.HasPrefix(call, "restore:") && call != wantCall {
			t.Fatalf("restore used the wrong commit or path: %q, want only %q", calls.all(), wantCall)
		}
	}
}

func TestCompareCommitPrefillsRefsAndStartsImmediately(t *testing.T) {
	tests := []struct {
		name     string
		parent   bool
		wantFrom string
		wantTo   string
		wantCall string
	}{
		{
			name:     "commit against HEAD",
			wantFrom: "c2c2c2c2",
			wantTo:   "HEAD",
			wantCall: "compare-files:c2c2c2c2:HEAD:false:3",
		},
		{
			name:     "first parent against commit",
			parent:   true,
			wantFrom: "c1c1c1c1",
			wantTo:   "c2c2c2c2",
			wantCall: "compare-files:c1c1c1c1:c2c2c2c2:false:3",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h, calls := menuTestAppWithCalls(t)
			h.app.compareCommit(h.app.snapshot.Commits[0], test.parent)

			if !h.app.compare.open {
				t.Fatal("the comparison did not open")
			}
			if h.app.compare.from != test.wantFrom || h.app.compare.to != test.wantTo {
				t.Fatalf("draft refs = %q to %q, want %q to %q",
					h.app.compare.from, h.app.compare.to, test.wantFrom, test.wantTo)
			}
			if h.app.compare.appliedFrom != test.wantFrom || h.app.compare.appliedTo != test.wantTo {
				t.Fatalf("applied refs = %q to %q, want an immediate comparison",
					h.app.compare.appliedFrom, h.app.compare.appliedTo)
			}
			if !h.app.compare.filesLoading {
				t.Fatal("compareCommit returned without starting its file-list read")
			}
			h.waitFor(t, "the immediate comparison", func() bool {
				return calls.has(test.wantCall) && len(h.app.compare.files) > 0 && !h.app.compare.filesLoading
			})
		})
	}
}

func TestCompareRootCommitToParentIsNoOp(t *testing.T) {
	h, calls := menuTestAppWithCalls(t)
	root := h.app.snapshot.Commits[len(h.app.snapshot.Commits)-1]
	if len(root.Parents) != 0 {
		t.Fatal("test fixture's last commit is not a root commit")
	}

	h.app.compareCommit(root, true)

	if h.app.compare.open || h.app.compare.from != "" || h.app.compare.to != "" || h.app.compare.filesLoading {
		t.Fatalf("root-parent comparison changed comparison state: %#v", h.app.compare)
	}
	for _, call := range calls.all() {
		if strings.HasPrefix(call, "compare-files:") {
			t.Fatalf("root-parent comparison started a read: %q", calls.all())
		}
	}
}
