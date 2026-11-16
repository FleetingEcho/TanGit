package repository

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemoteEffectsRequireExplicitCompensation(t *testing.T) {
	dir, session := workspaceRecoveryFixture(t)
	record := discardWorkspaceRecoveryFiles(t, dir, session)
	before, after := strings.Repeat("a", 40), strings.Repeat("b", 40)
	record.RemoteEffects = []RemoteRefEffect{{Remote: "origin", RefName: "refs/heads/main", BeforeOID: before, AfterOID: after}}
	if record.CanUndo() {
		t.Fatal("remote effects were presented as an implicit inverse operation")
	}
	name, err := session.(*gitSession).operationPath(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.(*gitSession).writeOperationRecord(name, record); err != nil {
		t.Fatal(err)
	}
	if err := session.UndoOperation(context.Background(), record.ID); !errors.Is(err, ErrRemoteCompensationRequired) {
		t.Fatalf("remote effect produced an implicit local Undo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); !os.IsNotExist(err) {
		t.Fatalf("remote compensation policy changed the discarded file: %v", err)
	}
	plans, err := record.RemoteCompensations()
	if err != nil || len(plans) != 1 {
		t.Fatalf("compensation plans = %#v, %v", plans, err)
	}
	plan := plans[0]
	if plan.ExpectedCurrentOID != after || plan.TargetOID != before || plan.Delete || !plan.RequiresConfirmation {
		t.Fatalf("unsafe compensation plan: %+v", plan)
	}
	record.RemoteEffects = []RemoteRefEffect{{Remote: "origin", RefName: "refs/heads/main", BeforeOID: before}}
	plans, err = record.RemoteCompensations()
	if err != nil || len(plans) != 1 || plans[0].ExpectedCurrentOID != "" || plans[0].TargetOID != before || plans[0].Delete {
		t.Fatalf("restoring a deleted remote ref has the wrong lease/target: %#v, %v", plans, err)
	}
	record.RemoteEffects = []RemoteRefEffect{{Remote: "origin", RefName: "refs/heads/new", AfterOID: after}}
	plans, err = record.RemoteCompensations()
	if err != nil || len(plans) != 1 || plans[0].ExpectedCurrentOID != after || plans[0].TargetOID != "" || !plans[0].Delete {
		t.Fatalf("removing a newly created remote ref has the wrong plan: %#v, %v", plans, err)
	}
}
