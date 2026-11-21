package app

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"lazygitapp/internal/agentwork"
	"lazygitapp/internal/repository"
)

func TestActivityFeedSourceNamespacesAndTargets(t *testing.T) {
	for _, source := range []activitySource{
		activitySourceCommand, activitySourceJournal, activitySourceAgent,
		activitySourceTransfer, activitySourceNotice,
	} {
		id := activityID{Source: source, Repo: "family", Value: "shared"}
		if !strings.HasPrefix(id.String(), string(source)+":") {
			t.Fatalf("ID %q lost its source namespace", id.String())
		}
		for _, other := range []activitySource{
			activitySourceCommand, activitySourceJournal, activitySourceAgent,
			activitySourceTransfer, activitySourceNotice,
		} {
			otherID := activityID{Source: other, Repo: "family", Value: "shared"}
			if source != other && id.String() == otherID.String() {
				t.Fatalf("source ID collision between %q and %q", source, other)
			}
		}
	}

	at := time.Date(2026, time.November, 22, 10, 0, 0, 0, time.UTC)
	input := activityAppInput{
		Repository: "family",
		Location:   "/work/main",
		Session:    "generation-1",
		Commands:   []repository.CommandEntry{{At: at, Command: "git status"}},
		Journal: []repository.OperationRecord{{
			ID: "0", Summary: "Reset", Status: repository.OperationSucceeded, CreatedAt: at.Unix(),
		}},
		Agents: []agentwork.Task{{
			ID: "task", Title: "Repair tests",
			Activity: []agentwork.Activity{{ID: "0", Kind: agentwork.ActivityLaunch, CreatedAt: at}},
		}},
		Transfers: []activityTransfer{{
			ID: "0", OccurredAt: at, Title: "Fetch", State: activityStateSucceeded, Severity: activitySeveritySuccess,
		}},
		Notices: []activityNotice{{
			ID: 0, OccurredAt: at, Severity: activitySeverityInfo, Title: "Opened",
			Target: activityTarget{Kind: activityTargetWorkspace, Command: commandID("activity/open")},
		}},
	}
	items := (activityFeed{}).Project(activityInputs{Apps: []activityAppInput{input}}, activityQuery{})
	if len(items) != 5 {
		t.Fatalf("projected %d rows, want 5: %#v", len(items), items)
	}
	seen := make(map[activitySource]activityItem, len(items))
	ids := make(map[string]bool, len(items))
	for _, item := range items {
		seen[item.Source] = item
		ids[item.ID.String()] = true
		if item.Target.RepositoryID != input.Location {
			t.Errorf("%s target location = %q, want %q", item.Source, item.Target.RepositoryID, input.Location)
		}
	}
	if len(ids) != 5 {
		t.Fatalf("source namespaced IDs collapsed: %#v", ids)
	}
	if got := seen[activitySourceCommand].Target; got.Kind != activityTargetCommand || got.PrimaryID != "0" {
		t.Errorf("unexpected command target: %#v", got)
	}
	if got := seen[activitySourceJournal].Target; got.Kind != activityTargetJournal || got.PrimaryID != "0" {
		t.Errorf("unexpected journal target: %#v", got)
	}
	if got := seen[activitySourceAgent].Target; got.Kind != activityTargetAgent || got.PrimaryID != "task" || got.SecondaryID != "0" {
		t.Errorf("unexpected agent target: %#v", got)
	}
	if got := seen[activitySourceTransfer].Target; got.Kind != activityTargetTransfer || got.PrimaryID != "0" {
		t.Errorf("unexpected transfer target: %#v", got)
	}
	if got := seen[activitySourceNotice].Target; got.Kind != activityTargetWorkspace || got.Command != commandID("activity/open") {
		t.Errorf("notice target was not preserved: %#v", got)
	}
}

func TestActivityFeedExactTimeTieSorting(t *testing.T) {
	at := time.Date(2026, time.November, 22, 11, 0, 0, 0, time.UTC)
	appInput := activityAppInput{
		Repository: "family", Location: "/work/main", Session: "one",
		Commands: []repository.CommandEntry{{At: at, Command: "status"}},
		Journal: []repository.OperationRecord{{
			ID: "journal", Summary: "operation", Status: repository.OperationSucceeded, CreatedAt: at.Unix(),
		}},
		Agents: []agentwork.Task{{
			ID: "task", Activity: []agentwork.Activity{{ID: "activity", Kind: agentwork.ActivityLaunch, CreatedAt: at}},
		}},
		Transfers: []activityTransfer{{
			ID: "transfer", OccurredAt: at, State: activityStateRunning, Severity: activitySeverityInfo,
		}},
		Notices: []activityNotice{
			{ID: 3, OccurredAt: at, Title: "three"},
			{ID: 20, OccurredAt: at, Title: "twenty"},
		},
	}
	items := (activityFeed{}).Project(activityInputs{Apps: []activityAppInput{appInput}}, activityQuery{})
	wantSources := []activitySource{
		activitySourceCommand, activitySourceJournal, activitySourceAgent,
		activitySourceTransfer, activitySourceNotice, activitySourceNotice,
	}
	if len(items) != len(wantSources) {
		t.Fatalf("got %d rows, want %d", len(items), len(wantSources))
	}
	for index, source := range wantSources {
		if items[index].Source != source {
			t.Errorf("row %d source = %q, want %q", index, items[index].Source, source)
		}
	}
	if items[4].ID.Value != "20" || items[5].ID.Value != "3" {
		t.Errorf("same-source ID tie was not lexical: %q then %q", items[4].ID.String(), items[5].ID.String())
	}
}

func TestActivityFeedRunningToTerminalKeepsCommandIdentity(t *testing.T) {
	log := repository.NewCommandLog()
	complete := log.Begin("git fetch origin")
	input := activityInputs{Apps: []activityAppInput{{
		Repository: "family", Location: "/work/main", Session: "logger-7", Commands: log.Entries(),
	}}}
	feed := activityFeed{}
	running := feed.Project(input, activityQuery{})
	if len(running) != 1 || running[0].State != activityStateRunning || running[0].Severity != activitySeverityInfo {
		t.Fatalf("unexpected running projection: %#v", running)
	}

	complete("remote summary\nsecond line is not projected", false)
	input.Apps[0].Commands = log.Entries()
	terminal := feed.Project(input, activityQuery{})
	if len(terminal) != 1 || terminal[0].State != activityStateSucceeded || terminal[0].Severity != activitySeveritySuccess {
		t.Fatalf("unexpected terminal projection: %#v", terminal)
	}
	if running[0].ID != terminal[0].ID || running[0].Target != terminal[0].Target {
		t.Errorf("command identity changed across completion: %#v -> %#v", running[0], terminal[0])
	}
	if terminal[0].Description != "remote summary" || strings.Contains(terminal[0].Description, "second line") {
		t.Errorf("command description is not a short output summary: %q", terminal[0].Description)
	}
}

func TestActivityFeedDurableRowsDedupAcrossMainAndLinkedWorktrees(t *testing.T) {
	at := time.Date(2026, time.November, 22, 12, 0, 0, 0, time.UTC)
	record := repository.OperationRecord{
		ID: "operation", Summary: "Discard changes", Status: repository.OperationFailed,
		Partial: true, CreatedAt: at.Unix(), FinishedAt: at.Add(time.Minute).Unix(), Error: "conflict",
	}
	task := agentwork.Task{
		ID: "task", Title: "Implement projection",
		Checkpoints: []agentwork.Checkpoint{{ID: "checkpoint-only", CreatedAt: at}},
		Activity: []agentwork.Activity{{
			ID: "activity", Kind: agentwork.ActivityStatus, StatusTo: string(agentwork.StatusBlocked), CreatedAt: at,
		}},
	}
	mainApp := activityAppInput{
		Repository: "agent-family", Location: "/work/main", Session: "main-session",
		Journal: []repository.OperationRecord{record}, Agents: []agentwork.Task{task},
	}
	linkedApp := activityAppInput{
		Repository: "agent-family", Location: "/work/linked", Session: "linked-session",
		Journal: []repository.OperationRecord{record}, Agents: []agentwork.Task{task},
	}
	originalAgents := []agentwork.Task{task.Clone()}
	forward := (activityFeed{}).Project(activityInputs{Apps: []activityAppInput{mainApp, linkedApp}}, activityQuery{})
	reversed := (activityFeed{}).Project(activityInputs{Apps: []activityAppInput{linkedApp, mainApp}}, activityQuery{})
	if len(forward) != 2 {
		t.Fatalf("durable duplicates produced %d rows, want 2: %#v", len(forward), forward)
	}
	if !reflect.DeepEqual(forward, reversed) {
		t.Errorf("durable dedup depends on app order:\n%#v\n%#v", forward, reversed)
	}
	for _, item := range forward {
		if item.Persistence != activityPersistenceDurable {
			t.Errorf("%s persistence = %q", item.Source, item.Persistence)
		}
	}
	if !reflect.DeepEqual(mainApp.Agents, originalAgents) {
		t.Error("projection mutated agent input")
	}
}

func TestActivityFeedCommandAndTransferSessionRowsNeverDedup(t *testing.T) {
	at := time.Date(2026, time.November, 22, 13, 0, 0, 0, time.UTC)
	apps := []activityAppInput{
		{Repository: "family", Location: "/work/main", Session: "one"},
		{Repository: "family", Location: "/work/main", Session: "reset-manager"},
		{Repository: "family", Location: "/work/linked", Session: "one"},
	}
	for index := range apps {
		apps[index].Commands = []repository.CommandEntry{{At: at, Command: "git status"}}
		apps[index].Transfers = []activityTransfer{{
			ID: "shared-transfer", OccurredAt: at, Title: "Fetch", State: activityStateRunning,
		}}
	}
	items := (activityFeed{}).Project(activityInputs{Apps: apps}, activityQuery{})
	if len(items) != 6 {
		t.Fatalf("session rows were merged across location/session: got %d, want 6", len(items))
	}
	ids := make(map[string]bool)
	for _, item := range items {
		ids[item.ID.String()] = true
		if item.Persistence != activityPersistenceSession {
			t.Errorf("%s persistence = %q", item.Source, item.Persistence)
		}
	}
	if len(ids) != 6 {
		t.Fatalf("session IDs are not unique: %#v", ids)
	}
}

func TestActivityFeedRepositorySourceSeverityAndTextFilters(t *testing.T) {
	at := time.Date(2026, time.November, 22, 14, 0, 0, 0, time.UTC)
	inputs := activityInputs{Apps: []activityAppInput{
		{
			Repository: "repo-a", Location: "/work/a", Session: "one",
			Commands: []repository.CommandEntry{{At: at, Command: "git push", Output: "Uploaded ALPHA"}},
			Journal: []repository.OperationRecord{{
				ID: "failed", Summary: "Reset beta", Status: repository.OperationFailed, CreatedAt: at.Unix(),
			}},
		},
		{
			Repository: "repo-b", Location: "/work/b", Session: "two",
			Notices: []activityNotice{{ID: 1, OccurredAt: at, Title: "Alpha elsewhere", Severity: activitySeverityInfo}},
		},
	}}
	feed := activityFeed{}
	tests := []struct {
		name   string
		query  activityQuery
		want   int
		source activitySource
	}{
		{name: "canonical repository", query: activityQuery{Repository: "repo-a"}, want: 2},
		{name: "target location", query: activityQuery{Repository: "/work/a"}, want: 2},
		{name: "source", query: activityQuery{Sources: map[activitySource]bool{activitySourceJournal: true}}, want: 1, source: activitySourceJournal},
		{name: "severity", query: activityQuery{Severities: map[activitySeverity]bool{activitySeverityError: true}}, want: 1, source: activitySourceJournal},
		{name: "case insensitive text", query: activityQuery{Repository: "repo-a", Text: "alpha"}, want: 1, source: activitySourceCommand},
		{
			name: "combined", query: activityQuery{
				Repository: "/work/a", Sources: map[activitySource]bool{activitySourceCommand: true},
				Severities: map[activitySeverity]bool{activitySeveritySuccess: true}, Text: "uploaded",
			}, want: 1, source: activitySourceCommand,
		},
		{name: "no match", query: activityQuery{Repository: "repo-a", Text: "missing"}, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.query.Limit = 50
			items := feed.Project(inputs, test.query)
			if len(items) != test.want {
				t.Fatalf("got %d items, want %d: %#v", len(items), test.want, items)
			}
			if test.source != "" && items[0].Source != test.source {
				t.Errorf("source = %q, want %q", items[0].Source, test.source)
			}
		})
	}
}

func TestActivityFeedDefaultAndCustomLimit(t *testing.T) {
	base := time.Date(2026, time.November, 22, 15, 0, 0, 0, time.UTC)
	appInput := activityAppInput{Repository: "family", Location: "/work/main", Session: "one"}
	for index := 0; index < 150; index++ {
		appInput.Notices = append(appInput.Notices, activityNotice{
			ID: uint64(index + 1), OccurredAt: base.Add(time.Duration(index) * time.Second), Title: "notice",
		})
	}
	inputs := activityInputs{Apps: []activityAppInput{appInput}}
	feed := activityFeed{}
	defaultItems := feed.Project(inputs, activityQuery{})
	if len(defaultItems) != 100 || defaultItems[0].ID.Value != "150" || defaultItems[99].ID.Value != "51" {
		t.Fatalf("default limit/order is wrong: len=%d first=%q last=%q", len(defaultItems), defaultItems[0].ID.Value, defaultItems[99].ID.Value)
	}
	custom := feed.Project(inputs, activityQuery{Limit: 17})
	if len(custom) != 17 || custom[0].ID.Value != "150" || custom[16].ID.Value != "134" {
		t.Fatalf("custom limit/order is wrong: len=%d first=%q last=%q", len(custom), custom[0].ID.Value, custom[16].ID.Value)
	}
	all := feed.Project(inputs, activityQuery{Limit: 500})
	if len(all) != 150 {
		t.Fatalf("custom limit larger than default returned %d rows, want 150", len(all))
	}
}

func TestActivityFeedPerAppAndGlobalCapsAreNewestAndDeterministic(t *testing.T) {
	base := time.Date(2026, time.November, 22, 16, 0, 0, 0, time.UTC)
	single := activityAppInput{Repository: "family", Location: "/work/single", Session: "one"}
	for index := 0; index < 650; index++ {
		description := "newest"
		if index < 150 {
			description = "needle"
		}
		single.Notices = append(single.Notices, activityNotice{
			ID: uint64(index), OccurredAt: base.Add(time.Duration(index) * time.Second),
			Title: "capped", Description: description,
		})
	}
	feed := activityFeed{}
	singleInputs := activityInputs{Apps: []activityAppInput{single}}
	capped := feed.Project(singleInputs, activityQuery{Limit: 1000})
	if len(capped) != 500 || capped[0].ID.Value != "649" || capped[499].ID.Value != "150" {
		t.Fatalf("per-app cap retained wrong rows: len=%d first=%q last=%q", len(capped), capped[0].ID.Value, capped[499].ID.Value)
	}
	for left, right := 0, len(single.Notices)-1; left < right; left, right = left+1, right-1 {
		single.Notices[left], single.Notices[right] = single.Notices[right], single.Notices[left]
	}
	reversed := feed.Project(activityInputs{Apps: []activityAppInput{single}}, activityQuery{Limit: 1000})
	if !reflect.DeepEqual(capped, reversed) {
		t.Error("per-app cap depends on input order")
	}
	if got := feed.Project(singleInputs, activityQuery{Text: "needle", Limit: 1000}); len(got) != 0 {
		t.Fatalf("filter scanned entries beyond the per-app cap: %d rows", len(got))
	}

	apps := make([]activityAppInput, 5)
	for appIndex := range apps {
		apps[appIndex] = activityAppInput{
			Repository: fmt.Sprintf("repo-%d", appIndex),
			Location:   fmt.Sprintf("/work/%d", appIndex),
			Session:    "one",
		}
		for noticeIndex := 0; noticeIndex < 500; noticeIndex++ {
			title := "retained"
			if appIndex == 0 {
				title = "needle"
			}
			apps[appIndex].Notices = append(apps[appIndex].Notices, activityNotice{
				ID:         uint64(noticeIndex),
				OccurredAt: base.Add(time.Duration(appIndex*1000+noticeIndex) * time.Second),
				Title:      title,
			})
		}
	}
	globalInputs := activityInputs{Apps: apps}
	global := feed.Project(globalInputs, activityQuery{Limit: 5000})
	if len(global) != 2000 {
		t.Fatalf("global cap returned %d rows, want 2000", len(global))
	}
	counts := make(map[string]int)
	for _, item := range global {
		counts[item.Repository]++
		if item.Repository == "repo-0" {
			t.Fatal("global cap retained an old candidate instead of a newer app candidate")
		}
	}
	for appIndex := 1; appIndex < 5; appIndex++ {
		if counts[fmt.Sprintf("repo-%d", appIndex)] != 500 {
			t.Errorf("repo-%d contributed %d rows, want 500", appIndex, counts[fmt.Sprintf("repo-%d", appIndex)])
		}
	}
	reversedApps := append([]activityAppInput(nil), apps...)
	for left, right := 0, len(reversedApps)-1; left < right; left, right = left+1, right-1 {
		reversedApps[left], reversedApps[right] = reversedApps[right], reversedApps[left]
	}
	if reversedGlobal := feed.Project(activityInputs{Apps: reversedApps}, activityQuery{Limit: 5000}); !reflect.DeepEqual(global, reversedGlobal) {
		t.Error("global cap depends on app order")
	}
	if got := feed.Project(globalInputs, activityQuery{Text: "needle", Limit: 5000}); len(got) != 0 {
		t.Fatalf("filter scanned entries beyond the global cap: %d rows", len(got))
	}
}

func TestActivityFeedRedactsEveryProjectedTextAdapter(t *testing.T) {
	at := time.Date(2026, time.November, 22, 17, 0, 0, 0, time.UTC)
	appInput := activityAppInput{
		Repository: "family", Location: "/work/main", Session: "one",
		Commands: []repository.CommandEntry{{
			At: at, Command: "git fetch --token cli-secret", Output: "first token=command-secret\nsecond token=second-secret",
		}},
		Journal: []repository.OperationRecord{{
			ID: "journal", Summary: "Reset token=journal-title-secret", Error: "token=journal-error-secret",
			Status: repository.OperationFailed, CreatedAt: at.Unix(),
		}},
		Agents: []agentwork.Task{{
			ID: "task", Title: "Work token=agent-title-secret",
			Activity: []agentwork.Activity{{
				ID: "activity", Kind: agentwork.ActivityLaunch, Command: "run --token agent-command-secret", CreatedAt: at,
			}},
		}},
		Transfers: []activityTransfer{{
			ID: "transfer", Title: "Upload token=transfer-title-secret", Description: "token=transfer-description-secret",
		}},
		Notices: []activityNotice{{
			ID: 1, Title: "Notice token=notice-title-secret", Description: "token=notice-description-secret",
		}},
	}
	items := (activityFeed{}).Project(activityInputs{Apps: []activityAppInput{appInput}}, activityQuery{Limit: 20})
	if len(items) != 5 {
		t.Fatalf("got %d redaction fixtures, want 5", len(items))
	}
	var projected strings.Builder
	for _, item := range items {
		projected.WriteString(item.Title)
		projected.WriteByte('\n')
		projected.WriteString(item.Description)
		projected.WriteByte('\n')
	}
	text := projected.String()
	for _, secret := range []string{
		"cli-secret", "command-secret", "second-secret", "journal-title-secret", "journal-error-secret",
		"agent-title-secret", "agent-command-secret", "transfer-title-secret", "transfer-description-secret",
		"notice-title-secret", "notice-description-secret",
	} {
		if strings.Contains(text, secret) {
			t.Errorf("projected text retained secret %q: %s", secret, text)
		}
	}
	if !strings.Contains(text, "[redacted]") {
		t.Fatalf("projected text has no redaction marker: %s", text)
	}
	if strings.Contains(items[0].Description, "second line") {
		t.Error("command output projection was not a summary")
	}
}

func TestActivityFeedAgentCheckpointsComeOnlyFromActivity(t *testing.T) {
	at := time.Date(2026, time.November, 22, 18, 0, 0, 0, time.UTC)
	task := agentwork.Task{
		ID: "task", Title: "Task",
		Checkpoints: []agentwork.Checkpoint{
			{ID: "checkpoint-1", CreatedAt: at},
			{ID: "checkpoint-2", CreatedAt: at.Add(time.Second)},
		},
		Activity: []agentwork.Activity{
			{ID: "activity-checkpoint", Kind: agentwork.ActivityCheckpoint, CheckpointID: "checkpoint-1", CreatedAt: at},
			{ID: "activity-status", Kind: agentwork.ActivityStatus, StatusTo: string(agentwork.StatusDone), CreatedAt: at},
		},
	}
	items := (activityFeed{}).Project(activityInputs{Apps: []activityAppInput{{
		Repository: "family", Location: "/work/main", Agents: []agentwork.Task{task},
	}}}, activityQuery{})
	if len(items) != len(task.Activity) {
		t.Fatalf("projected %d agent rows from %d activities and %d checkpoints", len(items), len(task.Activity), len(task.Checkpoints))
	}
	secondary := map[string]bool{}
	for _, item := range items {
		secondary[item.Target.SecondaryID] = true
		if item.Target.PrimaryID != task.ID {
			t.Errorf("agent target primary ID = %q, want task ID", item.Target.PrimaryID)
		}
	}
	if !secondary["activity-checkpoint"] || !secondary["activity-status"] || secondary["checkpoint-1"] || secondary["checkpoint-2"] {
		t.Errorf("checkpoint rows were duplicated or target identity changed: %#v", secondary)
	}
}

func TestActivityFeedCommandTargetUsesImmutableEntryID(t *testing.T) {
	log := repository.NewCommandLog()
	log.Record("git status", "one", false)
	log.Record("git status", "two", false)
	entries := log.Entries()
	if len(entries) != 2 || entries[0].ID() == entries[1].ID() {
		t.Fatalf("command log fixture lacks distinct immutable IDs: %#v", entries)
	}
	items := (activityFeed{}).Project(activityInputs{Apps: []activityAppInput{{
		Repository: "family", Location: "/work/main", Session: "logger", Commands: entries,
	}}}, activityQuery{})
	want := map[string]bool{}
	for _, entry := range entries {
		want[strconv.FormatUint(entry.ID(), 10)] = true
	}
	for _, item := range items {
		if item.Target.Kind != activityTargetCommand || !want[item.Target.PrimaryID] {
			t.Errorf("target does not use its immutable command entry ID: %#v", item.Target)
		}
	}

	previousID := entries[1].ID()
	log.Clear()
	log.Record("git status", "three", false)
	newEntry := log.Entries()[0]
	if newEntry.ID() == previousID {
		t.Fatal("clearing the command log reused an immutable ID")
	}
	if newEntry.ID() == entries[0].ID() || newEntry.ID() == entries[1].ID() {
		t.Fatal("same command text was treated as immutable identity")
	}
}

func TestActivityFeedUsesOnlyActualSourceTimestamps(t *testing.T) {
	record := repository.OperationRecord{
		ID: "journal", Summary: "operation", Status: repository.OperationSucceeded,
		CreatedAt: 100, FinishedAt: 200, UndoStartedAt: 300, UndoneAt: 400,
	}
	items := (activityFeed{}).Project(activityInputs{Apps: []activityAppInput{{
		Repository: "family", Location: "/work/main",
		Journal:   []repository.OperationRecord{record},
		Transfers: []activityTransfer{{ID: "transfer", Title: "running", State: activityStateRunning}},
		Agents:    []agentwork.Task{{ID: "task", Activity: []agentwork.Activity{{ID: "zero", Kind: agentwork.ActivityLaunch}}}},
	}}}, activityQuery{})
	if len(items) != 3 {
		t.Fatalf("got %d timestamp fixtures, want 3", len(items))
	}
	for _, item := range items {
		switch item.Source {
		case activitySourceJournal:
			if !item.OccurredAt.Equal(time.Unix(100, 0)) || !item.UpdatedAt.Equal(time.Unix(400, 0)) {
				t.Errorf("journal timestamps = %v / %v", item.OccurredAt, item.UpdatedAt)
			}
		case activitySourceTransfer:
			if !item.OccurredAt.IsZero() || !item.UpdatedAt.IsZero() {
				t.Errorf("zero transfer timestamps were invented: %v / %v", item.OccurredAt, item.UpdatedAt)
			}
		case activitySourceAgent:
			if !item.OccurredAt.IsZero() || !item.UpdatedAt.IsZero() {
				t.Errorf("zero agent timestamps were invented: %v / %v", item.OccurredAt, item.UpdatedAt)
			}
		}
	}
}

var activityProjectionBenchmarkResult []activityItem

func BenchmarkActivityProjection2000(b *testing.B) {
	base := time.Date(2026, time.November, 22, 19, 0, 0, 0, time.UTC)
	apps := make([]activityAppInput, 4)
	for appIndex := range apps {
		apps[appIndex] = activityAppInput{
			Repository: fmt.Sprintf("repo-%d", appIndex), Location: fmt.Sprintf("/work/%d", appIndex), Session: "one",
		}
		for index := 0; index < 500; index++ {
			apps[appIndex].Notices = append(apps[appIndex].Notices, activityNotice{
				ID: uint64(index), OccurredAt: base.Add(time.Duration(appIndex*500+index) * time.Second),
				Severity: activitySeverityInfo, Title: "Activity", Description: "Projection benchmark",
			})
		}
	}
	inputs := activityInputs{Apps: apps}
	query := activityQuery{Limit: 2000}
	feed := activityFeed{}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		activityProjectionBenchmarkResult = feed.Project(inputs, query)
		if len(activityProjectionBenchmarkResult) != 2000 {
			b.Fatalf("projected %d rows, want 2000", len(activityProjectionBenchmarkResult))
		}
	}
}
