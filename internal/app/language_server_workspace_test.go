package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRustWorkspaceLinksNestedProjectsAndLooseFiles(t *testing.T) {
	root := t.TempDir()
	for _, relative := range []string{"apps/native/Cargo.toml", "apps/native/src/main.rs", "samples/loose.rs"} {
		full := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	options := rustLanguageServerInitializationOptions(root, nil, []string{"apps/native/src/main.rs", "samples/loose.rs"})
	want := []string{filepath.Join(root, "apps", "native", "Cargo.toml"), filepath.Join(root, "samples", "loose.rs")}
	if !reflect.DeepEqual(options["linkedProjects"], want) {
		t.Fatalf("Rust projects=%#v, want %v", options, want)
	}
	loose := rustLanguageServerInitializationOptions(root, nil, []string{"samples/loose.rs"})
	if loose["checkOnSave"] != false {
		t.Fatal("standalone Rust requires nightly cargo checks")
	}
}

func TestManagedLanguageServerDeduplicatesWorkspaceErrors(t *testing.T) {
	h := menuTestApp(t)
	profile, _ := languageServerProfileByID("rust")
	server := &managedLanguageServer{profile: profile, sequence: 1}
	h.app.additionalLanguageServers = map[string]*managedLanguageServer{"rust": server}
	raw, _ := json.Marshal(map[string]any{"type": 1, "message": "Failed to discover workspace. Failed to load workspaces."})
	for range 2 {
		h.app.handleManagedLanguageServerNotification(server, 1, "window/showMessage", raw)
		h.updates.waitForUpdate(t)
	}
	if len(h.app.pendingToasts) != 1 {
		t.Fatalf("duplicate workspace notifications=%d", len(h.app.pendingToasts))
	}
}
