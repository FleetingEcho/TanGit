package repository

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lazygitapp/internal/sshintegration"
	"lazygitapp/internal/sshworkspace"
	"lazygitapp/internal/workspace"
)

// This is the remote counterpart of the local live-todo integration test. The
// repository is real Git on the OpenSSH test server, not a host-side session
// or a fake filesystem. Environments without the test server's working auth
// still surface the handshake failure rather than silently skipping it.
func TestOpenSSHWorkspaceVisualRebasePlanRealOpenSSH(t *testing.T) {
	server := sshintegration.Start(t)
	connector, err := sshworkspace.NewConnector(server.SecretStore, filepath.Join(t.TempDir(), "known_hosts"), func(sshworkspace.HostKeyInfo) (bool, error) {
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	client, err := connector.Connect(ctx, server.Profile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	dir := t.TempDir()
	runGit(t, dir, "init", "--initial-branch=main")
	runGit(t, dir, "config", "user.name", "TanGit Test")
	runGit(t, dir, "config", "user.email", "tangit@example.com")
	runGit(t, dir, "config", "rebase.updateRefs", "true")
	hashes := map[string]string{}
	for index, subject := range []string{"root", "first", "second"} {
		name := filepath.Join(dir, subject+".txt")
		if err := os.WriteFile(name, []byte(subject+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, dir, "add", "--", subject+".txt")
		runGit(t, dir, "commit", "-m", subject)
		hashes[subject] = strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
		if index == 1 {
			runGit(t, dir, "branch", "stack/topic")
		}
	}
	session, err := OpenSSHWorkspace(ctx, client, workspace.Location{
		Kind: workspace.SSH, ProfileID: server.Profile.ID, Root: dir,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.(interface{ Close() error }).Close()
	plans := rebasePlanSession(t, session)
	plan, err := plans.PrepareRebasePlan(ctx, RebasePlanOptions{TargetRef: "HEAD~2"})
	if err != nil {
		t.Fatalf("prepare remote plan: %v", err)
	}
	var firstPick *OperationStep
	foundUpdateRef := false
	for index := range plan.Steps {
		step := &plan.Steps[index]
		if step.Action == "update-ref" && step.UpdateRef == "refs/heads/stack/topic" {
			foundUpdateRef = true
		}
		if firstPick == nil && step.Action == "pick" {
			firstPick = step
		}
	}
	if firstPick == nil || !foundUpdateRef {
		t.Fatalf("remote draft lacks pick/update-ref: %#v", plan.Steps)
	}
	edited := strings.Replace(plan.Text, firstPick.Raw, rebaseActionForRepositoryTest(firstPick.Raw, "edit"), 1)
	if err := plans.StartRebasePlan(ctx, plan, edited); err != nil {
		t.Fatalf("start remote plan: %v", err)
	}
	state, err := plans.OperationState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	currentFound := false
	for _, step := range state.Steps {
		if step.Status == OperationStepCurrent && step.Action == "edit" && step.Hash == hashes["first"] && step.Subject == "first" {
			currentFound = true
		}
	}
	if !currentFound {
		t.Fatalf("remote live steps lack the full current first commit: %#v", state.Steps)
	}
	active, err := plans.ReadRebasePlan(ctx)
	if err != nil {
		t.Fatalf("read remote active todo: %v", err)
	}
	var pendingPick *OperationStep
	for index := range active.Steps {
		if active.Steps[index].Action == "pick" {
			pendingPick = &active.Steps[index]
			break
		}
	}
	if pendingPick == nil {
		t.Fatalf("remote active todo has no pending pick: %#v", active.Steps)
	}
	edited = strings.Replace(active.Text, pendingPick.Raw, rebaseActionForRepositoryTest(pendingPick.Raw, "reword"), 1)
	if err := plans.EditRebasePlan(ctx, active, edited); err != nil {
		t.Fatalf("remote whole-todo CAS: %v", err)
	}
	updated, err := plans.OperationState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rewordFound := false
	for _, step := range updated.Steps {
		if step.Status == OperationStepPending && step.Action == "reword" && step.Hash == hashes["second"] {
			rewordFound = true
		}
	}
	if !rewordFound {
		t.Fatalf("remote OperationState did not follow the todo CAS: %#v", updated.Steps)
	}
	if err := plans.DriveOperation(ctx, OperationDriveOptions{
		Action: OperationActionContinue, ExpectedFingerprint: updated.Fingerprint,
	}); err != nil {
		t.Fatalf("continue remote plan through driver: %v", err)
	}
	finished, err := plans.OperationState(ctx)
	if err != nil || finished.Type != OperationTypeNone {
		t.Fatalf("remote plan remains after continue: %#v, %v", finished, err)
	}
	if got := strings.TrimSpace(gitOutput(t, dir, "log", "--format=%s", "-1", "stack/topic")); got != "first" {
		t.Fatalf("remote update-ref subject = %q, want first", got)
	}
}

func rebaseActionForRepositoryTest(raw, action string) string {
	_, rest := splitRebaseInstruction(raw)
	return action + " " + rest
}
