package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/agentassist"
	"lazygitapp/internal/repository"
)

type appFakeAdvisor struct {
	mu       sync.Mutex
	requests []agentassist.AdviceRequest
	respond  func(agentassist.AdviceRequest) (agentassist.Advice, error)
	started  chan struct{}
	release  chan struct{}
	finished chan struct{}
}

func (f *appFakeAdvisor) Propose(ctx context.Context, request agentassist.AdviceRequest) (agentassist.Advice, error) {
	if f.finished != nil {
		defer close(f.finished)
	}
	f.mu.Lock()
	f.requests = append(f.requests, request.Clone())
	f.mu.Unlock()
	if f.started != nil {
		select {
		case f.started <- struct{}{}:
		default:
		}
	}
	if f.release != nil {
		<-f.release // intentionally may outlive ctx to test generation rejection
	}
	if err := ctx.Err(); err != nil && f.release == nil {
		return agentassist.Advice{}, err
	}
	return f.respond(request)
}

func (f *appFakeAdvisor) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func appConflictAdvice(request agentassist.AdviceRequest, substitution agentassist.Substitution) agentassist.Advice {
	advice := agentassist.Advice{Version: agentassist.ProtocolVersion, Kind: request.Kind, SourceDigest: request.SourceDigest}
	artifact := request.Artifacts[0]
	advice.Artifacts = []agentassist.ArtifactIdentity{{ID: artifact.ID, Digest: artifact.Digest}}
	for index, region := range artifact.Regions {
		advice.Suggestions = append(advice.Suggestions, agentassist.Suggestion{
			ID: "region-" + string(rune('a'+index)), Summary: "matches the reviewed current implementation",
			Conflict: &agentassist.ConflictSuggestion{ArtifactID: artifact.ID, RegionID: region.ID, Substitution: substitution},
		})
	}
	return advice
}

type fakeConflictResultSession struct {
	fakeSession
	mu       sync.Mutex
	requests []repository.ConflictResultRequest
	err      error
}

func (s *fakeConflictResultSession) WriteConflictResult(_ context.Context, request repository.ConflictResultRequest) (repository.FileRecoveryRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	request.Result = bytes.Clone(request.Result)
	s.requests = append(s.requests, request)
	if s.err != nil {
		return repository.FileRecoveryRecord{}, s.err
	}
	return repository.FileRecoveryRecord{Version: repository.FileRecoveryManifestVersion, OperationID: "recovery-test"}, nil
}

func (s *fakeConflictResultSession) requestCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}

const appConflictSource = "prefix\n<<<<<<< HEAD\ncurrent\n||||||| base\nancestor\n=======\nincoming\n>>>>>>> feature\nsuffix\n"

func conflictAssistantHarness(t *testing.T, width, height int) (*harness, *callRecorder) {
	t.Helper()
	originalRead, originalWrite := readWorkingFile, writeWorkingFile
	t.Cleanup(func() { readWorkingFile, writeWorkingFile = originalRead, originalWrite })
	readWorkingFile = func(string) ([]byte, error) { return []byte(appConflictSource), nil }
	writes := &callRecorder{}
	writeWorkingFile = func(_ string, data []byte) error {
		writes.record("write:" + string(data))
		return nil
	}
	stages := repository.ConflictStages{
		Base:     repository.ConflictStage{Present: true, Content: "ancestor\n"},
		Current:  repository.ConflictStage{Present: true, Content: "current\n"},
		Incoming: repository.ConflictStage{Present: true, Content: "incoming\n"},
	}
	snapshot := repository.Snapshot{
		Root: "/work/project", Branch: "main", Operation: "merge",
		OperationState: &repository.OperationState{Type: repository.OperationTypeMerge, Fingerprint: "operation-reviewed"},
		Files:          []repository.File{{Path: "conflict.txt", Status: "UU", Tracked: true, Unstaged: true, HasMergeConflicts: true}},
	}
	calls := &callRecorder{}
	a := newApp(fakeOpener{result: openResult{snapshot: snapshot}, calls: calls, conflictStages: stages})
	a.workspaceTab = sectionChanges
	h := &harness{app: a, updates: newUpdates()}
	h.tester = ui.NewTester(a.view, width, height)
	a.update = h.updates.update
	a.path = "/work/project"
	openFixture(t, h.tester)
	h.settle(t)
	h.waitFor(t, "Conflict Assistant source", func() bool { return a.conflictState != nil })
	a.conflictState.undo = [][]string{{"prior undo must survive"}}
	return h, writes
}

func TestConflictAssistantDoesNothingWithoutExplicitConfiguredRequest(t *testing.T) {
	h, writes := conflictAssistantHarness(t, 900, 700)
	factoryCalls := 0
	h.app.assistance.advisorFactory = func(string) (agentassist.Advisor, error) {
		factoryCalls++
		t.Fatal("default empty AssistCommand constructed an advisor")
		return nil, nil
	}
	h.app.openConflictAssistant()
	h.tester.Frame()
	if factoryCalls != 0 || len(writes.all()) != 0 || !h.tester.HasText("No suggestions yet") {
		t.Fatalf("opening the modal had side effects: factory=%d writes=%q", factoryCalls, writes.all())
	}
	h.app.beginConflictAdvice()
	h.tester.Frame()
	if factoryCalls != 0 || !strings.Contains(h.app.assistance.conflict.err, "no AssistCommand") {
		t.Fatalf("unconfigured request error=%q factory=%d", h.app.assistance.conflict.err, factoryCalls)
	}
	if h.app.conflictState.text() != appConflictSource {
		t.Fatal("unconfigured assistance changed Result")
	}
}

func TestConflictAssistantClonePreviewAndAcceptOnlyResult(t *testing.T) {
	h, writes := conflictAssistantHarness(t, 1000, 760)
	advisor := &appFakeAdvisor{respond: func(request agentassist.AdviceRequest) (agentassist.Advice, error) {
		return appConflictAdvice(request, agentassist.SubstituteCurrent), nil
	}}
	h.app.assistance.advisor = advisor
	repositoryCalls := &callRecorder{}
	writer := &fakeConflictResultSession{fakeSession: fakeSession{snapshot: h.app.snapshot, conflictStages: h.app.conflictStages, calls: repositoryCalls}}
	h.app.session = writer
	source := h.app.conflictState
	originalLines := append([]string(nil), source.originalLines...)
	undo := append([][]string(nil), source.undo...)

	h.app.openConflictAssistant()
	h.app.beginConflictAdvice()
	h.waitFor(t, "validated Conflict Assistant preview", func() bool { return h.app.assistance.conflict.preview != nil })
	assist := h.app.assistance.conflict
	if assist.clone == source || assist.clone.text() != "prefix\ncurrent\nsuffix\n" {
		t.Fatalf("clone Result = %q", assist.clone.text())
	}
	if assist.diff == nil || assist.source.WorktreeDigest == "" || assist.source.IndexDigest == "" || assist.source.OperationFingerprint != "operation-reviewed" {
		t.Fatalf("missing diff/source binding: %#v", assist)
	}
	if source.text() != appConflictSource || !slicesEqualStrings(source.originalLines, originalLines) || len(source.undo) != len(undo) || source.undo[0][0] != undo[0][0] {
		t.Fatal("preview changed source lines, originalLines, or ordinary undo")
	}
	if len(writes.all()) != 0 || advisor.calls() != 1 {
		t.Fatalf("preview side effects: writes=%q advisor calls=%d", writes.all(), advisor.calls())
	}
	h.tester.SetSize(640, 540)
	h.tester.Frame()
	acceptBounds, found := h.tester.Find("Accept Result only")
	if !found || acceptBounds.X < 0 || acceptBounds.Y < 0 || acceptBounds.X+acceptBounds.W > 640 || acceptBounds.Y+acceptBounds.H > 540 {
		t.Fatalf("narrow Accept action is clipped: found=%v bounds=%+v", found, acceptBounds)
	}
	h.tester.SetSize(1000, 760)
	h.tester.Frame()

	h.app.acceptConflictAdvice()
	h.waitFor(t, "Result-only accept", func() bool { return !h.app.actionBusy && writer.requestCount() == 1 })
	writer.mu.Lock()
	request := writer.requests[0]
	writer.mu.Unlock()
	if string(request.Result) != "prefix\ncurrent\nsuffix\n" || request.WorktreeDigest != assist.source.WorktreeDigest || request.IndexDigest != assist.source.IndexDigest || request.OperationFingerprint != "operation-reviewed" {
		t.Fatalf("accepted request = %#v", request)
	}
	for _, forbidden := range []string{"stage:conflict.txt", "commit:", "apply-patch:"} {
		for _, call := range repositoryCalls.all() {
			if strings.HasPrefix(call, forbidden) {
				t.Fatalf("accept invoked forbidden Git write %q; all calls=%q", call, repositoryCalls.all())
			}
		}
	}
	if source.text() != appConflictSource || len(source.undo) != 1 || source.undo[0][0] != undo[0][0] {
		t.Fatal("accept mutated the old source/undo instead of publishing a fresh Result load")
	}
}

func slicesEqualStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func TestConflictAssistantRejectsMalformedAdviceAndWriteHashMismatch(t *testing.T) {
	t.Run("malformed Advice", func(t *testing.T) {
		h, writes := conflictAssistantHarness(t, 900, 700)
		h.app.assistance.advisor = &appFakeAdvisor{respond: func(request agentassist.AdviceRequest) (agentassist.Advice, error) {
			advice := appConflictAdvice(request, agentassist.SubstituteCurrent)
			advice.SourceDigest = "sha256:" + strings.Repeat("0", 64)
			return advice, nil
		}}
		h.app.openConflictAssistant()
		h.app.beginConflictAdvice()
		h.waitFor(t, "malformed Advice rejection", func() bool { return !h.app.assistance.conflict.running && h.app.assistance.conflict.err != "" })
		if h.app.assistance.conflict.preview != nil || h.app.conflictAdviceComplete() {
			t.Fatal("malformed Advice became acceptable")
		}
		h.app.acceptConflictAdvice()
		if len(writes.all()) != 0 || h.app.conflictState.text() != appConflictSource {
			t.Fatal("malformed Advice changed the source")
		}
	})

	t.Run("repository hash mismatch", func(t *testing.T) {
		h, _ := conflictAssistantHarness(t, 900, 700)
		h.app.assistance.advisor = &appFakeAdvisor{respond: func(request agentassist.AdviceRequest) (agentassist.Advice, error) {
			return appConflictAdvice(request, agentassist.SubstituteIncoming), nil
		}}
		writer := &fakeConflictResultSession{fakeSession: fakeSession{snapshot: h.app.snapshot}, err: repository.ErrConflictResultChanged}
		h.app.session = writer
		source := h.app.conflictState
		h.app.openConflictAssistant()
		h.app.beginConflictAdvice()
		h.waitFor(t, "hash mismatch preview", func() bool { return h.app.assistance.conflict.preview != nil })
		h.app.acceptConflictAdvice()
		h.waitFor(t, "hash mismatch refusal", func() bool { return !h.app.actionBusy && writer.requestCount() == 1 })
		if source.text() != appConflictSource {
			t.Fatal("hash mismatch changed the reviewed Result source")
		}
	})
}

func TestConflictAssistantCancelRejectsLateAdviceWithoutWrites(t *testing.T) {
	h, writes := conflictAssistantHarness(t, 640, 540)
	advisor := &appFakeAdvisor{started: make(chan struct{}, 1), release: make(chan struct{}), finished: make(chan struct{})}
	// Use a valid custom substitution explicitly; this delayed response remains
	// well-formed so only cancellation/generation can reject it.
	advisor.respond = func(request agentassist.AdviceRequest) (agentassist.Advice, error) {
		advice := appConflictAdvice(request, agentassist.SubstituteCustom)
		for index := range advice.Suggestions {
			advice.Suggestions[index].Conflict.Custom = "delayed replacement\n"
		}
		return advice, nil
	}
	h.app.assistance.advisor = advisor
	h.app.openConflictAssistant()
	h.app.beginConflictAdvice()
	h.tester.Frame()
	<-advisor.started
	if !h.tester.HasText("Requesting suggestions…") {
		h.tester.Frame()
	}
	h.app.cancelConflictAdviceRequest()
	close(advisor.release)
	<-advisor.finished
	h.updates.waitForUpdate(t)
	h.tester.Frame()
	h.settle(t)
	if h.app.assistance.conflict.preview != nil || h.app.conflictAdviceComplete() {
		t.Fatal("late Advice survived cancellation")
	}
	h.app.acceptConflictAdvice()
	if len(writes.all()) != 0 || h.app.conflictState.text() != appConflictSource {
		t.Fatal("canceled Advice wrote a Result")
	}
	// The bounded modal still keeps its actions inside a narrow viewport.
	h.tester.Frame()
	bounds, found := h.tester.Find("Get region suggestions")
	if !found || bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.W > 640 || bounds.Y+bounds.H > 540 {
		t.Fatalf("narrow modal action is clipped: found=%v bounds=%+v", found, bounds)
	}
}

func TestConflictAssistantRealProfileHelperUsesIsolatedPreviewOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX test helper")
	}
	h, writes := conflictAssistantHarness(t, 940, 700)
	request, err := agentassist.NewRequest(agentassist.KindConflict, []agentassist.Artifact{{
		Path: "conflict.txt", Text: appConflictSource, MarkerSize: defaultConflictMarkerSize,
	}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := json.Marshal(appConflictAdvice(request, agentassist.SubstituteCurrent))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	helper := filepath.Join(root, "region advisor.sh")
	capture := filepath.Join(root, "helper paths")
	script := "#!/bin/sh\nset -eu\n" +
		"[ -f \"$1\" ]\n[ \"$PWD\" = \"$3\" ]\n" +
		"printf '%s\\n' \"$1\" \"$2\" \"$3\" > " + shellQuote(capture) + "\n" +
		"cat > \"$2\" <<'TANGIT_RESPONSE'\n" + string(response) + "\nTANGIT_RESPONSE\n"
	if err := os.WriteFile(helper, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	h.app.agents.assistCommand = shellQuote(helper) + " {requestFile} {responseFile} {artifactDir}"
	h.app.openConflictAssistant()
	if _, err := os.Stat(capture); !os.IsNotExist(err) {
		t.Fatalf("helper ran while opening Conflict Assistant: %v", err)
	}
	h.app.beginConflictAdvice()
	h.waitFor(t, "real JSON-file helper preview", func() bool { return h.app.assistance.conflict.preview != nil })
	if h.app.conflictState.text() != appConflictSource || len(writes.all()) != 0 {
		t.Fatal("real helper preview changed the source working tree")
	}
	pathBytes, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	paths := strings.Split(strings.TrimSpace(string(pathBytes)), "\n")
	if len(paths) != 3 || filepath.Base(paths[0]) != "request.json" || filepath.Base(paths[1]) != "response.json" || filepath.Base(paths[2]) != "artifacts" {
		t.Fatalf("helper paths are not the isolated protocol paths: %q", paths)
	}
	for _, name := range paths {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Fatalf("per-run helper path survived validation: %s (%v)", name, err)
		}
	}
	if _, err := os.Stat(filepath.Dir(paths[0])); !os.IsNotExist(err) {
		t.Fatalf("private helper directory survived: %v", err)
	}
}
