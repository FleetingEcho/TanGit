package app

import (
	"context"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/agentassist"
	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

type fakeChangeSplitSession struct {
	fakeSession
	workingPatch string
}

func (s *fakeChangeSplitSession) WorkingTreeFilePatch(_ context.Context, file repository.File) (string, error) {
	s.calls.record("working-tree-file-patch:" + file.Path)
	return s.workingPatch, nil
}

func changeSplitHarness(t *testing.T, path, patch string) (*harness, *fakeChangeSplitSession) {
	t.Helper()
	snapshot := repository.Snapshot{
		Root: "/work/project", Branch: "main",
		Files: []repository.File{{Path: path, Status: " M", Unstaged: true, Tracked: true}},
	}
	calls := &callRecorder{}
	session := &fakeChangeSplitSession{fakeSession: fakeSession{snapshot: snapshot, patch: patch, calls: calls}, workingPatch: patch}
	a := newApp(fixedSessionOpener{session: session})
	a.workspaceTab = sectionChanges
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 940, 700)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	return h, session
}

func appSplitResponse(request agentassist.AdviceRequest) agentassist.Advice {
	advice := agentassist.Advice{Version: agentassist.ProtocolVersion, Kind: request.Kind, SourceDigest: request.SourceDigest}
	artifact := request.Artifacts[0]
	advice.Artifacts = []agentassist.ArtifactIdentity{{ID: artifact.ID, Digest: artifact.Digest}}
	return advice
}

func TestChangeSplitterUsesOriginalIdentitySummariesExclusionsAndOneGroup(t *testing.T) {
	h, session := changeSplitHarness(t, "changed.txt", fakeWorkingTreePatch)
	h.app.assistance.advisor = &appFakeAdvisor{respond: func(request agentassist.AdviceRequest) (agentassist.Advice, error) {
		advice := appSplitResponse(request)
		artifact := &request.Artifacts[0]
		file := artifact.Files[0]
		hunk := file.Hunks[0]
		firstIDs := []string{hunk.Lines[1].ID, hunk.Lines[2].ID} // -two, +TWO
		advice.Suggestions = []agentassist.Suggestion{{
			ID: "two-only", Summary: "isolate the TWO replacement",
			Split: &agentassist.ChangeSplit{Changes: []agentassist.ChangeRef{{
				ArtifactID: artifact.ID, FileID: file.ID, HunkID: hunk.ID, LineIDs: firstIDs,
			}}},
		}}
		for _, index := range []int{4, 5} { // -four, +FOUR
			advice.Exclusions = append(advice.Exclusions, agentassist.Exclusion{
				ArtifactID: artifact.ID, FileID: file.ID, HunkID: hunk.ID, LineID: hunk.Lines[index].ID,
				Reason: "human review FOUR separately",
			})
		}
		return advice, nil
	}}
	h.app.openChangeSplitter()
	h.app.beginChangeSplitAdvice()
	h.waitFor(t, "validated split groups", func() bool { return len(h.app.assistance.splitter.groups) == 1 })
	groups := h.app.assistance.splitter.groups
	if groups[0].summary != "isolate the TWO replacement" || groups[0].preview == nil || !groups[0].preview.Clean {
		t.Fatalf("group = %#v", groups[0])
	}
	if !strings.Contains(groups[0].patch, "-two") || !strings.Contains(groups[0].patch, "+TWO") || strings.Contains(groups[0].patch, "FOUR") {
		t.Fatalf("group patch includes the wrong identities:\n%s", groups[0].patch)
	}
	h.tester.Frame()
	if !h.tester.HasText("human review FOUR separately") || !h.tester.HasText("Explicitly excluded source identities") {
		t.Fatalf("exclusion reasons are not visible: %q", h.tester.Texts())
	}

	h.app.applyChangeSplitGroup(0)
	h.waitFor(t, "explicit single-group index apply", func() bool { return !h.app.actionBusy })
	var applied int
	for _, call := range session.calls.all() {
		if strings.HasPrefix(call, "apply-patch:false:") {
			applied++
			if strings.Contains(call, "FOUR") || !strings.Contains(call, "+TWO") {
				t.Fatalf("applied wrong group: %s", call)
			}
		}
		if strings.HasPrefix(call, "stage:") || strings.HasPrefix(call, "commit:") || strings.HasPrefix(call, "apply-worktree:") {
			t.Fatalf("splitter invoked a forbidden mutation: %s", call)
		}
	}
	if applied != 1 {
		t.Fatalf("index apply calls = %d, calls=%q", applied, session.calls.all())
	}
}

func TestChangeSplitterDuplicateAdviceAndLateCanceledResponseCannotApply(t *testing.T) {
	t.Run("duplicate Advice", func(t *testing.T) {
		h, session := changeSplitHarness(t, "changed.txt", fakeWorkingTreePatch)
		h.app.assistance.advisor = &appFakeAdvisor{respond: func(request agentassist.AdviceRequest) (agentassist.Advice, error) {
			advice := appSplitResponse(request)
			artifact := request.Artifacts[0]
			file := artifact.Files[0]
			hunk := file.Hunks[0]
			ref := agentassist.ChangeRef{ArtifactID: artifact.ID, FileID: file.ID, HunkID: hunk.ID}
			advice.Suggestions = []agentassist.Suggestion{
				{ID: "one", Summary: "duplicate one", Split: &agentassist.ChangeSplit{Changes: []agentassist.ChangeRef{ref}}},
				{ID: "two", Summary: "duplicate two", Split: &agentassist.ChangeSplit{Changes: []agentassist.ChangeRef{ref}}},
			}
			return advice, nil
		}}
		h.app.openChangeSplitter()
		h.app.beginChangeSplitAdvice()
		h.waitFor(t, "duplicate Advice rejection", func() bool { return !h.app.assistance.splitter.running && h.app.assistance.splitter.err != "" })
		if len(h.app.assistance.splitter.groups) != 0 {
			t.Fatal("duplicate identities produced applicable groups")
		}
		h.app.applyChangeSplitGroup(0)
		for _, call := range session.calls.all() {
			if strings.HasPrefix(call, "apply-patch:") || strings.HasPrefix(call, "stage:") {
				t.Fatalf("malformed Advice caused a write: %s", call)
			}
		}
	})

	t.Run("cancel stale generation", func(t *testing.T) {
		h, session := changeSplitHarness(t, "changed.txt", fakeWorkingTreePatch)
		advisor := &appFakeAdvisor{started: make(chan struct{}, 1), release: make(chan struct{}), finished: make(chan struct{})}
		advisor.respond = func(request agentassist.AdviceRequest) (agentassist.Advice, error) {
			advice := appSplitResponse(request)
			artifact := request.Artifacts[0]
			file := artifact.Files[0]
			advice.Suggestions = []agentassist.Suggestion{{
				ID: "late", Summary: "late whole file", Split: &agentassist.ChangeSplit{Changes: []agentassist.ChangeRef{{
					ArtifactID: artifact.ID, FileID: file.ID,
				}}},
			}}
			return advice, nil
		}
		h.app.assistance.advisor = advisor
		h.app.openChangeSplitter()
		h.app.beginChangeSplitAdvice()
		<-advisor.started
		h.app.cancelChangeSplitRequest()
		close(advisor.release)
		<-advisor.finished
		h.updates.waitForUpdate(t)
		h.tester.Frame()
		if len(h.app.assistance.splitter.groups) != 0 {
			t.Fatal("late split groups survived cancellation")
		}
		h.app.applyChangeSplitGroup(0)
		for _, call := range session.calls.all() {
			if strings.HasPrefix(call, "apply-patch:") || strings.HasPrefix(call, "stage:") {
				t.Fatalf("canceled split caused a write: %s", call)
			}
		}
	})
}

func TestChangeSplitterBinaryIsLocalWholeFileWithoutConfiguration(t *testing.T) {
	const patch = "diff --git a/image.bin b/image.bin\nindex 1111111..2222222 100644\nBinary files a/image.bin and b/image.bin differ\n"
	h, session := changeSplitHarness(t, "image.bin", patch)
	factoryCalls := 0
	h.app.assistance.advisorFactory = func(string) (agentassist.Advisor, error) {
		factoryCalls++
		t.Fatal("binary whole-file preview consulted external AI")
		return nil, nil
	}
	h.app.openChangeSplitter()
	h.app.beginChangeSplitAdvice()
	h.waitFor(t, "binary local whole-file preview", func() bool { return len(h.app.assistance.splitter.groups) == 1 })
	groups := h.app.assistance.splitter.groups
	if !groups[0].local || groups[0].patch != patch || groups[0].preview == nil || !groups[0].preview.Clean {
		t.Fatalf("binary group = %#v", groups[0])
	}
	if factoryCalls != 0 || len(h.app.assistance.splitter.exclusions) != 0 {
		t.Fatalf("binary local preview factory=%d exclusions=%d", factoryCalls, len(h.app.assistance.splitter.exclusions))
	}
	h.tester.SetSize(640, 540)
	h.tester.Frame()
	if !h.tester.HasText("Binary content is never sent to the text helper and can only be applied as a whole file.") {
		t.Fatalf("binary whole-file explanation absent: %q", h.tester.Texts())
	}
	applyBounds, found := h.tester.Find("Apply group to index")
	if !found || applyBounds.X < 0 || applyBounds.Y < 0 || applyBounds.X+applyBounds.W > 640 || applyBounds.Y+applyBounds.H > 540 {
		t.Fatalf("narrow binary Apply is clipped: found=%v bounds=%+v", found, applyBounds)
	}
	h.app.applyChangeSplitGroup(0)
	h.waitFor(t, "binary whole-file explicit apply", func() bool { return !h.app.actionBusy })
	applied := false
	for _, call := range session.calls.all() {
		if strings.HasPrefix(call, "apply-patch:false:") && strings.Contains(call, "Binary files") {
			applied = true
		}
	}
	if !applied {
		t.Fatalf("binary file was not applied as one canonical whole-file patch: %q", session.calls.all())
	}
}

func TestChangeSplitterUIApplyStagesOnlyOneRealGitGroup(t *testing.T) {
	edited := strings.NewReplacer("two\n", "TWO\n", "sixteen\n", "sixteen\nsixteen and a half\n").Replace(realHunkBase)
	dir, session := newRealHunkRepository(t, edited)
	a := newApp(fixedSessionOpener{session: session})
	a.workspaceTab = sectionChanges
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1100, 760)
	a.update = h.updates.update
	a.path = dir
	openFixture(t, h.tester)
	h.settle(t)
	h.waitFor(t, "real unstaged Changes source", func() bool {
		selected, ok := a.selectedChangeSplitFile()
		return ok && selected.Path == "lines.txt"
	})

	a.assistance.advisor = &appFakeAdvisor{respond: func(request agentassist.AdviceRequest) (agentassist.Advice, error) {
		advice := appSplitResponse(request)
		artifact := request.Artifacts[0]
		file := artifact.Files[0]
		for index, hunk := range file.Hunks {
			advice.Suggestions = append(advice.Suggestions, agentassist.Suggestion{
				ID: "hunk-" + string(rune('a'+index)), Summary: "stage hunk " + string(rune('1'+index)),
				Split: &agentassist.ChangeSplit{Changes: []agentassist.ChangeRef{{
					ArtifactID: artifact.ID, FileID: file.ID, HunkID: hunk.ID,
				}}},
			})
		}
		return advice, nil
	}}
	a.openChangeSplitter()
	a.beginChangeSplitAdvice()
	h.waitFor(t, "two real Git split previews", func() bool { return len(a.assistance.splitter.groups) == 2 })
	for _, group := range a.assistance.splitter.groups {
		if group.preview == nil || !group.preview.Clean || group.previewError != "" {
			t.Fatalf("real Git group is not cleanly previewed: %#v", group)
		}
	}
	firstPatch := a.assistance.splitter.groups[0].patch
	if !strings.Contains(firstPatch, "+TWO") || strings.Contains(firstPatch, "sixteen and a half") {
		t.Fatalf("first group has the wrong original hunk:\n%s", firstPatch)
	}
	beforeHEAD := runGitIn(t, dir, "rev-parse", "HEAD")
	h.tester.Frame()
	if err := h.tester.Click("Apply group to index"); err != nil {
		t.Fatalf("click first split Apply: %v", err)
	}
	h.tester.Frame()
	h.waitFor(t, "one real index-only split Apply and deferred metadata", func() bool {
		return !a.actionBusy && !a.repositoryDetailsLoading
	})
	if a.assistance.splitter.open {
		t.Fatal("successful explicit Apply left the reviewed modal open")
	}
	if !a.quietRefreshAllowed() {
		t.Fatal("successful explicit Apply did not restore quiet refresh")
	}

	wantIndex := strings.Replace(realHunkBase, "two\n", "TWO\n", 1)
	if got := runGitIn(t, dir, "show", ":lines.txt"); got != wantIndex {
		t.Fatalf("index after first group:\n%s\nwant only first hunk:\n%s", got, wantIndex)
	}
	if got := readRealHunkFile(t, dir); got != edited {
		t.Fatal("index-only split Apply changed the working tree")
	}
	if got := runGitIn(t, dir, "rev-parse", "HEAD"); got != beforeHEAD {
		t.Fatal("split Apply created or moved a commit")
	}
}

func TestChangeSplitterUIApplyUsesReviewedPreviewAfterPublicMutationAndExternalChange(t *testing.T) {
	edited := strings.NewReplacer("two\n", "TWO\n", "sixteen\n", "sixteen\nsixteen and a half\n").Replace(realHunkBase)
	dir, session := newRealHunkRepository(t, edited)
	a := newApp(fixedSessionOpener{session: session})
	a.workspaceTab = sectionChanges
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, 1100, 760)
	a.update = h.updates.update
	a.path = dir
	openFixture(t, h.tester)
	h.settle(t)
	h.waitFor(t, "real unstaged Changes source", func() bool {
		selected, ok := a.selectedChangeSplitFile()
		return ok && selected.Path == "lines.txt"
	})

	a.assistance.advisor = &appFakeAdvisor{respond: func(request agentassist.AdviceRequest) (agentassist.Advice, error) {
		advice := appSplitResponse(request)
		artifact := request.Artifacts[0]
		file := artifact.Files[0]
		for index, hunk := range file.Hunks {
			advice.Suggestions = append(advice.Suggestions, agentassist.Suggestion{
				ID: "hunk-" + string(rune('a'+index)), Summary: "stage hunk " + string(rune('1'+index)),
				Split: &agentassist.ChangeSplit{Changes: []agentassist.ChangeRef{{
					ArtifactID: artifact.ID, FileID: file.ID, HunkID: hunk.ID,
				}}},
			})
		}
		return advice, nil
	}}
	a.openChangeSplitter()
	a.beginChangeSplitAdvice()
	h.waitFor(t, "two reviewed real Git previews", func() bool { return len(a.assistance.splitter.groups) == 2 })
	groups := a.assistance.splitter.groups
	reviewed := groups[0].preview
	if reviewed == nil || !reviewed.Clean || groups[1].preview == nil || !groups[1].preview.Clean {
		t.Fatalf("real Git previews are not clean: %#v", groups)
	}

	// Public patch text and display options are not mutation authority. Make
	// them name the other group and another operation, then change the source
	// as an external editor would. Apply must still consume the original
	// private ticket and fail that ticket's state comparison before any write.
	a.assistance.splitter.groups[0].patch = groups[1].patch
	a.assistance.splitter.groups[0].preview.Text = groups[1].patch
	a.assistance.splitter.groups[0].preview.Target = repository.PatchWorktree
	a.assistance.splitter.groups[0].preview.Reverse = true
	external := edited + "external editor\n"
	writeRealHunkFile(t, dir, external)
	indexBefore := runGitIn(t, dir, "show", ":lines.txt")
	headBefore := runGitIn(t, dir, "rev-parse", "HEAD")

	h.tester.Frame()
	if !a.assistance.splitter.open || a.quietRefreshAllowed() {
		t.Error("the open real preview did not suspend quiet refresh before the explicit Apply")
	}
	if err := h.tester.Click("Apply group to index"); err != nil {
		t.Fatalf("click stale split Apply: %v", err)
	}
	h.tester.Frame()
	h.waitFor(t, "stale reviewed preview refusal and deferred metadata", func() bool {
		return !a.actionBusy && !a.repositoryDetailsLoading
	})

	if !strings.Contains(a.actionError, "repository changed since patch preview") {
		t.Errorf("stale Apply action error = %q", a.actionError)
	}
	if got := runGitIn(t, dir, "show", ":lines.txt"); got != indexBefore {
		t.Errorf("stale Apply changed the index:\n%s", got)
	}
	if got := readRealHunkFile(t, dir); got != external {
		t.Error("stale Apply overwrote the external editor's working-tree changes")
	}
	if got := runGitIn(t, dir, "rev-parse", "HEAD"); got != headBefore {
		t.Error("stale Apply created or moved a commit")
	}
	if !a.quietRefreshAllowed() {
		t.Error("the explicit Apply did not restore quiet refresh after the refusal")
	}
	h.tester.Frame()
	if !h.tester.HasText("repository changed since patch preview") {
		t.Errorf("CAS refusal is not visible in the real UI: %q", h.tester.Texts())
	}
}
