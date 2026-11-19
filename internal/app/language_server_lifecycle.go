package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const managedLanguageServerManifestFile = ".tangit-language-server.json"

type managedLanguageServerManifest struct {
	Format int    `json:"format"`
	ID     string `json:"id"`
}

var probeLanguageServerInstallation = func(installation languageServerInstallation) (string, error) {
	return runLanguageServerVersionArguments(installation.Path, nil, installation.ProbeArguments)
}

var removeManagedLanguageServerDirectory = os.RemoveAll

func managedLanguageServerRoot(dataDir, id string) string {
	return filepath.Join(dataDir, "tools", "lsp", id)
}

func newManagedLanguageServerStaging(dataDir, id string) (string, string, error) {
	parent := filepath.Join(dataDir, "tools", "lsp")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", "", err
	}
	root := managedLanguageServerRoot(dataDir, id)
	staging, err := os.MkdirTemp(parent, "."+id+"-install-")
	if err != nil {
		return "", "", err
	}
	return staging, root, nil
}

func validManagedLanguageServerStaging(id, root string) bool {
	if root == "" || filepath.Base(filepath.Dir(root)) != "lsp" ||
		filepath.Base(filepath.Dir(filepath.Dir(root))) != "tools" {
		return false
	}
	return strings.HasPrefix(filepath.Base(root), "."+id+"-install-")
}

func cleanupManagedLanguageServerStaging(id, root string) {
	if validManagedLanguageServerStaging(id, root) {
		_ = os.RemoveAll(root)
	}
}

func prepareLanguageServerInstallation(profile languageServerProfile, dataDir string,
	installation languageServerInstallation) (languageServerInstallation, string, error) {
	if installation.OwnedRoot == "" && !installation.ProbeRequired {
		// Compatibility for the existing configuration-only installer seam. It
		// can never confer ownership merely by returning a destination path.
		installation.ManagedRoot = ""
		return installation, "", nil
	}
	if installation.OwnedRoot != "" &&
		(installation.ManagedRoot != managedLanguageServerRoot(dataDir, profile.ID) ||
			filepath.Dir(installation.OwnedRoot) != filepath.Dir(installation.ManagedRoot)) {
		return languageServerInstallation{}, "", errors.New("managed language-server installer returned inconsistent roots")
	}
	version, err := probeLanguageServerInstallation(installation)
	if err != nil {
		cleanupManagedLanguageServerStaging(profile.ID, installation.OwnedRoot)
		return languageServerInstallation{}, "", fmt.Errorf("validate installed language server: %w", err)
	}
	version = shortLanguageServerVersion(version)
	if installation.OwnedRoot == "" {
		// Rustup owns its toolchain and files. A successful probe validates the
		// component but deliberately creates no TanGit deletion ownership.
		installation.ManagedRoot = ""
		return installation, version, nil
	}
	if err := publishManagedLanguageServerInstallation(profile.ID, dataDir, &installation); err != nil {
		cleanupManagedLanguageServerStaging(profile.ID, installation.OwnedRoot)
		return languageServerInstallation{}, "", err
	}
	return installation, version, nil
}

func shortLanguageServerVersion(version string) string {
	for _, line := range strings.Split(strings.ReplaceAll(version, "\r\n", "\n"), "\n") {
		line = strings.Map(func(r rune) rune {
			if r < 32 || r == 127 {
				return -1
			}
			return r
		}, line)
		if line = strings.TrimSpace(line); line != "" {
			return shorten(line, 160)
		}
	}
	return ""
}

func publishManagedLanguageServerInstallation(id, dataDir string, installation *languageServerInstallation) error {
	if installation == nil || !validManagedLanguageServerStaging(id, installation.OwnedRoot) {
		return errors.New("managed language-server installer returned an invalid staging directory")
	}
	expected := managedLanguageServerRoot(dataDir, id)
	if installation.ManagedRoot != expected || !validManagedLanguageServerRoot(id, expected) {
		return errors.New("managed language-server installer returned an invalid destination")
	}
	manifest, err := json.Marshal(managedLanguageServerManifest{Format: 1, ID: id})
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(installation.OwnedRoot, managedLanguageServerManifestFile), manifest, 0o600); err != nil {
		return err
	}
	backup := ""
	if info, err := os.Lstat(expected); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("managed language-server destination is not a safe directory")
		}
		if _, err := readManagedLanguageServerManifest(expected, id); err != nil {
			return fmt.Errorf("preserve unrecognized existing language-server directory: %w", err)
		}
		backup, err = os.MkdirTemp(filepath.Dir(expected), "."+id+"-previous-")
		if err != nil {
			return err
		}
		if err := os.Remove(backup); err != nil {
			return err
		}
		if err := os.Rename(expected, backup); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(installation.OwnedRoot, expected); err != nil {
		if backup != "" {
			_ = os.Rename(backup, expected)
		}
		return err
	}
	rebaseLanguageServerInstallation(installation, expected)
	if backup != "" {
		// backup is a renamed directory whose manifest was verified above.
		_ = os.RemoveAll(backup)
	}
	return nil
}

func rebaseLanguageServerInstallation(installation *languageServerInstallation, root string) {
	oldRoot := installation.OwnedRoot
	rebase := func(path string) string {
		if !pathWithin(oldRoot, path) {
			return path
		}
		relative, err := filepath.Rel(oldRoot, path)
		if err != nil {
			return path
		}
		return filepath.Join(root, relative)
	}
	installation.Path = rebase(installation.Path)
	for index := range installation.Arguments {
		installation.Arguments[index] = rebase(installation.Arguments[index])
	}
	for index := range installation.ProbeArguments {
		installation.ProbeArguments[index] = rebase(installation.ProbeArguments[index])
	}
	installation.OwnedRoot = ""
	installation.ManagedRoot = root
}

func readManagedLanguageServerManifest(root, id string) (managedLanguageServerManifest, error) {
	var manifest managedLanguageServerManifest
	file, err := os.Open(filepath.Join(root, managedLanguageServerManifestFile))
	if err != nil {
		return manifest, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 64<<10))
	if err := decoder.Decode(&manifest); err != nil {
		return manifest, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return manifest, errors.New("language-server ownership manifest has trailing data")
	}
	if manifest.Format != 1 || manifest.ID != id {
		return manifest, errors.New("language-server ownership manifest does not match")
	}
	return manifest, nil
}

func (a *app) languageServerLifecycleBusy() bool {
	for _, busy := range []map[string]bool{a.lsp.installing, a.lsp.uninstalling} {
		for _, active := range busy {
			if active {
				return true
			}
		}
	}
	return false
}

func (a *app) ownedManagedLanguageServer(id string) bool {
	profile, ok := languageServerProfileByID(id)
	server := a.settings.LanguageServers[id]
	dataDir := a.dataDir()
	return ok && !profile.Custom && id != "rust" && server.Custom == nil && server.Managed != nil && dataDir != "" &&
		server.Managed.Root == managedLanguageServerRoot(dataDir, id) && validManagedLanguageServerRoot(id, server.Managed.Root) &&
		languageServerSettingsReferenceRoot(server, server.Managed.Root)
}

func (a *app) removeManagedLanguageServer(id string) {
	profile, ok := languageServerProfileByID(id)
	if !ok || !a.ownedManagedLanguageServer(id) || a.lsp.installing[id] || a.lsp.uninstalling[id] {
		return
	}
	root := a.settings.LanguageServers[id].Managed.Root
	a.confirmAttention("Remove managed language server", profile.Name,
		"Only TanGit's managed copy will be deleted. The enabled preference will be kept and PATH can be used afterwards.",
		func() { a.beginManagedLanguageServerRemoval(id, root) })
}

func (a *app) beginManagedLanguageServerRemoval(id, root string) {
	profile, ok := languageServerProfileByID(id)
	server := a.settings.LanguageServers[id]
	if !ok || a.lsp.installing[id] || a.lsp.uninstalling[id] || !a.ownedManagedLanguageServer(id) ||
		server.Managed.Root != root {
		return
	}
	if a.lsp.uninstalling == nil {
		a.lsp.uninstalling = make(map[string]bool)
	}
	a.lsp.uninstalling[id] = true
	connection := a.stopLanguageServerForLifecycle(id)
	a.notifyTranslated("info", trf("Removing %s", profile.Name), "Removing TanGit's managed language-server copy…")
	dataDir := a.dataDir()
	go func() {
		defer a.recoverBackground()
		waitForLanguageServerClose(connection)
		err := func() error {
			languageServerInstallMu.Lock()
			defer languageServerInstallMu.Unlock()
			if err := validateManagedLanguageServerDeletion(profile, dataDir, root, server); err != nil {
				return err
			}
			// This is the sole per-server uninstall deletion. Validation above
			// requires explicit persisted ownership and a matching manifest.
			return removeManagedLanguageServerDirectory(root)
		}()
		a.update(func() {
			delete(a.lsp.uninstalling, id)
			if err != nil {
				a.syncGoLanguageServer()
				a.notify("error", trf("Could not remove %s", profile.Name), safeError(err))
				return
			}
			current := a.settings.LanguageServers[id]
			current.Path = ""
			current.Arguments = nil
			current.ProbeArguments = nil
			current.Managed = nil
			// Disabled is intentionally untouched. Built-in profiles are never
			// custom here, and a blank path restores normal executable fallback.
			if a.settings.LanguageServers == nil {
				a.settings.LanguageServers = make(map[string]languageServerSettings)
			}
			a.settings.LanguageServers[id] = current
			a.saveSettings()
			a.loadGoLanguageServerDraft()
			a.notifyTranslated("success", trf("%s removed", profile.Name), "TanGit's managed copy was removed.")
			a.syncGoLanguageServer()
		})
	}()
}

func validateManagedLanguageServerDeletion(profile languageServerProfile, dataDir, root string,
	server languageServerSettings) error {
	if profile.Custom || server.Custom != nil || profile.ID == "rust" || server.Managed == nil ||
		server.Managed.Root != root || root != managedLanguageServerRoot(dataDir, profile.ID) ||
		!validManagedLanguageServerRoot(profile.ID, root) || !languageServerSettingsReferenceRoot(server, root) {
		return errors.New("language server is not a TanGit-owned isolated installation")
	}
	info, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("managed language-server root is not a safe directory")
	}
	_, err = readManagedLanguageServerManifest(root, profile.ID)
	return err
}

func (a *app) stopLanguageServerForLifecycle(id string) languageServerConnection {
	if id == goLanguageServerID {
		connection := a.lsp.goServer
		a.stopGoLanguageServer()
		return connection
	}
	server := a.lsp.additional[id]
	if server == nil {
		return nil
	}
	connection := server.connection
	a.stopManagedLanguageServer(server)
	delete(a.lsp.additional, id)
	return connection
}

func waitForLanguageServerClose(connection languageServerConnection) {
	if connection == nil {
		return
	}
	select {
	case <-connection.Done():
	case <-time.After(4 * time.Second):
	}
}
