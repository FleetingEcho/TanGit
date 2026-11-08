package app

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"lazygitapp/internal/sshworkspace"
)

const (
	sshAuthAgent    = "agent"
	sshAuthKey      = "key"
	sshAuthPassword = "password"
	maxSSHProfileID = 64
)

var errSSHSecretSessionOnly = errors.New("SSH credential is available only for this session")

// sshProfileDraft is deliberately separate from appSettings. Password and
// Passphrase live here only while the form is open, then move to the keychain
// (or the session-only fallback) and are cleared from the draft.
type sshProfileDraft struct {
	ID             string
	Name           string
	Host           string
	Username       string
	Port           string
	AuthMode       string
	PrivateKeyPath string
	DefaultRoot    string
	Password       string
	Passphrase     string
}

type sshProfileSecrets struct {
	Password   string
	Passphrase string
}

// sshKeychain is the small part of mygo.Secrets used by the profile store.
// Keeping it injectable lets tests exercise keychain failure without touching
// the developer's real credential store.
type sshKeychain interface {
	Set(account string, value []byte) error
	Get(account string) ([]byte, error)
	Delete(account string) error
}

// appSSHSecretStore adapts the app's keychain and session fallback to the
// connector's stable SecretRef interface. The app pointer is never serialized.
type appSSHSecretStore struct{ app *app }

func (a *app) sshSecrets() sshworkspace.SecretStore {
	return appSSHSecretStore{app: a}
}

func (a *app) sshSessionSecretState() (*sync.RWMutex, *map[string]string) {
	if opener, ok := a.opener.(appSSHRepositoryOpener); ok && opener.base != nil {
		return &opener.base.sshSecretsMu, &opener.base.sshSessionSecrets
	}
	// Tests and custom local-only shells may not use the composite opener. Keep
	// the old app-local behavior there rather than imposing SSH infrastructure.
	return &a.sshSecretsMu, &a.sshSessionSecrets
}

func (a *app) sshKeychain() sshKeychain {
	if a.sshSecretBackend != nil {
		return a.sshSecretBackend
	}
	return mygo.Secrets
}

func (s appSSHSecretStore) Get(ref sshworkspace.SecretRef) (string, bool, error) {
	if ref == "" {
		return "", false, errors.New("invalid SSH secret reference")
	}
	account := string(ref)
	mutex, values := s.app.sshSessionSecretState()
	mutex.RLock()
	value, sessionValue := (*values)[account]
	mutex.RUnlock()
	if sessionValue {
		// An empty session value is a tombstone: it shadows a value that could
		// not be removed from an unavailable keychain.
		return value, value != "", nil
	}
	valueBytes, err := s.app.sshKeychain().Get(account)
	if err == nil {
		return string(valueBytes), true, nil
	}
	if errors.Is(err, mygo.ErrSecretNotFound) {
		return "", false, nil
	}
	return "", false, errors.New("SSH system keychain is unavailable")
}

func (s appSSHSecretStore) Set(ref sshworkspace.SecretRef, value string) error {
	if ref == "" {
		return errors.New("invalid SSH secret reference")
	}
	account := string(ref)
	err := s.app.sshKeychain().Set(account, []byte(value))
	mutex, values := s.app.sshSessionSecretState()
	mutex.Lock()
	defer mutex.Unlock()
	if err == nil {
		delete(*values, account)
		return nil
	}
	if *values == nil {
		*values = make(map[string]string)
	}
	// The fallback shadows any older keychain value. Never return the
	// keychain's error text, which belongs to the host credential service.
	(*values)[account] = value
	return errSSHSecretSessionOnly
}

func (s appSSHSecretStore) Delete(ref sshworkspace.SecretRef) error {
	if ref == "" {
		return errors.New("invalid SSH secret reference")
	}
	account := string(ref)
	err := s.app.sshKeychain().Delete(account)
	mutex, values := s.app.sshSessionSecretState()
	mutex.Lock()
	defer mutex.Unlock()
	if err == nil {
		delete(*values, account)
		return nil
	}
	if *values == nil {
		*values = make(map[string]string)
	}
	(*values)[account] = ""
	return errSSHSecretSessionOnly
}

func (a *app) sshCredentials(profileID string) sshProfileSecrets {
	password, _, _ := a.sshSecrets().Get(sshworkspace.PasswordSecretRef(profileID))
	passphrase, _, _ := a.sshSecrets().Get(sshworkspace.PassphraseSecretRef(profileID))
	return sshProfileSecrets{Password: password, Passphrase: passphrase}
}

// saveSSHDraftSecrets returns false when at least one value had to use the
// session fallback. It never puts a credential in an error or in settings.
func (a *app) saveSSHDraftSecrets(profileID string, draft sshProfileDraft) bool {
	store := a.sshSecrets()
	persistent := true
	switch sshworkspace.AuthMode(draft.AuthMode) {
	case sshworkspace.AuthPassword:
		if draft.Password != "" {
			if err := store.Set(sshworkspace.PasswordSecretRef(profileID), draft.Password); err != nil {
				persistent = false
			}
		}
		_ = store.Delete(sshworkspace.PassphraseSecretRef(profileID))
	case sshworkspace.AuthKey:
		if draft.Passphrase != "" {
			if err := store.Set(sshworkspace.PassphraseSecretRef(profileID), draft.Passphrase); err != nil {
				persistent = false
			}
		}
		_ = store.Delete(sshworkspace.PasswordSecretRef(profileID))
	default:
		_ = store.Delete(sshworkspace.PasswordSecretRef(profileID))
		_ = store.Delete(sshworkspace.PassphraseSecretRef(profileID))
	}
	return persistent
}

func (a *app) deleteSSHProfileSecrets(profileID string) {
	store := a.sshSecrets()
	_ = store.Delete(sshworkspace.PasswordSecretRef(profileID))
	_ = store.Delete(sshworkspace.PassphraseSecretRef(profileID))
}

func (a *app) clearSSHProfileSecrets() {
	for _, profile := range a.settings.SSHProfiles {
		a.deleteSSHProfileSecrets(profile.ID)
	}
}

func newSSHProfileDraft() sshProfileDraft {
	return sshProfileDraft{Port: strconv.Itoa(sshworkspace.DefaultPort), Username: "root", AuthMode: sshAuthPassword, DefaultRoot: "/home"}
}

func sshDraftFromProfile(profile sshworkspace.Profile) sshProfileDraft {
	port := profile.Port
	if port == 0 {
		port = sshworkspace.DefaultPort
	}
	return sshProfileDraft{
		ID:             profile.ID,
		Name:           profile.Name,
		Host:           profile.Host,
		Username:       profile.Username,
		Port:           strconv.Itoa(port),
		AuthMode:       string(profile.AuthMode),
		PrivateKeyPath: profile.PrivateKeyPath,
		DefaultRoot:    profile.DefaultRoot,
	}
}

func (a *app) sshDraftProfile(draft sshProfileDraft) (sshworkspace.Profile, error) {
	port, err := strconv.Atoi(strings.TrimSpace(draft.Port))
	if err != nil || port < 1 || port > 65535 {
		return sshworkspace.Profile{}, errors.New("Enter an SSH port from 1 to 65535.")
	}
	mode := sshworkspace.AuthMode(draft.AuthMode)
	profile := sshworkspace.Profile{
		ID:          draft.ID,
		Name:        strings.TrimSpace(draft.Name),
		Host:        strings.TrimSpace(draft.Host),
		Username:    strings.TrimSpace(draft.Username),
		Port:        port,
		AuthMode:    mode,
		DefaultRoot: strings.TrimSpace(draft.DefaultRoot),
	}
	if profile.Name == "" {
		profile.Name = profile.Host
	}
	if mode == sshworkspace.AuthKey {
		profile.PrivateKeyPath = strings.TrimSpace(draft.PrivateKeyPath)
	}
	if profile.ID == "" {
		profile.ID = uniqueSSHProfileID(profile.Name, a.settings.SSHProfiles, "")
	}
	if err := profile.Validate(); err != nil {
		return sshworkspace.Profile{}, err
	}
	if mode == sshworkspace.AuthPassword {
		password := draft.Password
		if password == "" && draft.ID != "" {
			password = a.sshCredentials(draft.ID).Password
		}
		if password == "" {
			return sshworkspace.Profile{}, errors.New("Enter an SSH password.")
		}
	}
	return profile, nil
}

func (a *app) openSSHProfileForm(profileID string) {
	a.cancelSSHWorkspaceResolve()
	a.cancelSSHProfileTest()
	a.sshProfileFormOpen = true
	a.sshProfileFormError = ""
	a.sshProfileFormNotice = ""
	if profileID == "" {
		a.sshProfileDraft = newSSHProfileDraft()
		return
	}
	for _, profile := range a.settings.SSHProfiles {
		if profile.ID == profileID {
			a.sshProfileDraft = sshDraftFromProfile(profile)
			return
		}
	}
	a.sshProfileDraft = newSSHProfileDraft()
}

func (a *app) closeSSHProfileForm() {
	a.cancelSSHProfileTest()
	a.sshProfileFormOpen = false
	a.sshProfileDraft = sshProfileDraft{}
	a.sshProfileFormError = ""
	a.sshProfileFormNotice = ""
}

func (a *app) submitSSHProfileForm(c *ui.Context) {
	profile, err := a.sshDraftProfile(a.sshProfileDraft)
	if err != nil {
		a.sshProfileFormError = err.Error()
		a.sshProfileFormNotice = ""
		return
	}
	draft := a.sshProfileDraft
	updated := false
	for i := range a.settings.SSHProfiles {
		if a.settings.SSHProfiles[i].ID == profile.ID {
			a.settings.SSHProfiles[i] = profile
			updated = true
			break
		}
	}
	if !updated {
		a.settings.SSHProfiles = append(a.settings.SSHProfiles, profile)
	}
	persistent := a.saveSSHDraftSecrets(profile.ID, draft)
	a.saveSettings()
	a.selectSSHWorkspaceProfile(profile.ID)
	a.closeSSHProfileForm()
	a.refreshSSHBrowserOnSelection()
	if !persistent {
		a.sshWorkspaceError = "Profile saved. Its new credential is available only for this session because the system keychain is unavailable."
	}
	if c != nil {
		c.Invalidate()
	}
}

func (a *app) deleteSSHProfile(profileID string) {
	kept := make([]sshworkspace.Profile, 0, len(a.settings.SSHProfiles))
	for _, profile := range a.settings.SSHProfiles {
		if profile.ID != profileID {
			kept = append(kept, profile)
		}
	}
	if len(kept) == len(a.settings.SSHProfiles) {
		return
	}
	a.settings.SSHProfiles = kept
	a.deleteSSHProfileSecrets(profileID)
	if a.sshWorkspaceProfileID == profileID {
		a.sshWorkspaceProfileID = ""
		a.sshWorkspaceRoot = ""
	}
	a.saveSettings()
}

// normalizeSSHProfiles gives every profile a deterministic, stable identity.
// Existing valid IDs survive; only malformed or duplicate IDs are repaired.
func normalizeSSHProfiles(profiles []sshworkspace.Profile) []sshworkspace.Profile {
	if len(profiles) == 0 {
		return nil
	}
	normalized := make([]sshworkspace.Profile, 0, len(profiles))
	for _, profile := range profiles {
		profile.Name = strings.TrimSpace(profile.Name)
		profile.Host = strings.TrimSpace(profile.Host)
		profile.Username = strings.TrimSpace(profile.Username)
		profile.PrivateKeyPath = strings.TrimSpace(profile.PrivateKeyPath)
		profile.DefaultRoot = strings.TrimSpace(profile.DefaultRoot)
		if profile.Port <= 0 || profile.Port > 65535 {
			profile.Port = sshworkspace.DefaultPort
		}
		switch profile.AuthMode {
		case sshworkspace.AuthAgent, sshworkspace.AuthKey, sshworkspace.AuthPassword:
		default:
			profile.AuthMode = sshworkspace.AuthAgent
		}
		if profile.AuthMode != sshworkspace.AuthKey {
			profile.PrivateKeyPath = ""
		}
		base := profile.ID
		if base == "" {
			base = profile.Name
		}
		if base == "" {
			base = profile.Host
		}
		profile.ID = uniqueSSHProfileID(base, normalized, "")
		normalized = append(normalized, profile)
	}
	return normalized
}

func uniqueSSHProfileID(base string, profiles []sshworkspace.Profile, keepID string) string {
	base = strings.TrimSpace(base)
	if !sshworkspace.ValidProfileID(base) || len(base) > maxSSHProfileID {
		base = normalizeSSHProfileID(base)
	}
	if base == "" {
		base = "profile"
	}
	used := make(map[string]bool, len(profiles))
	for _, profile := range profiles {
		if profile.ID != keepID {
			used[profile.ID] = true
		}
	}
	if !used[base] {
		return base
	}
	for suffix := 2; ; suffix++ {
		tail := "-" + strconv.Itoa(suffix)
		stem := strings.TrimRight(base[:min(len(base), maxSSHProfileID-len(tail))], "-._")
		if stem == "" {
			stem = "profile"
		}
		candidate := stem + tail
		if !used[candidate] {
			return candidate
		}
	}
}

func normalizeSSHProfileID(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var id strings.Builder
	separator := false
	for _, r := range value {
		alphaNumeric := r >= 'a' && r <= 'z' || r >= '0' && r <= '9'
		if alphaNumeric || r == '.' || r == '_' || r == '-' {
			if id.Len() == 0 && !alphaNumeric {
				continue
			}
			id.WriteRune(r)
			separator = false
			continue
		}
		if id.Len() > 0 && !separator {
			id.WriteByte('-')
			separator = true
		}
		if id.Len() >= maxSSHProfileID {
			break
		}
	}
	return strings.TrimRight(id.String()[:min(id.Len(), maxSSHProfileID)], "-._")
}

func (a *app) openSSHWorkspacePicker() {
	a.cancelSSHWorkspaceResolve()
	a.loadSettings()
	a.sshWorkspaceOpen = true
	a.sshWorkspaceProfileID = ""
	a.sshWorkspaceRoot = "/home"
	a.sshBrowser = sshBrowserState{}
	a.sshWorkspaceError = ""
	a.sshProfileFormError = ""
	a.sshProfileFormNotice = ""
	if len(a.settings.SSHProfiles) == 0 {
		a.openSSHProfileForm("")
		return
	}
}

func (a *app) cancelSSHWorkspaceResolve() {
	a.sshWorkspaceResolveSeq++
	a.sshWorkspaceResolving = false
	if a.sshWorkspaceResolveCancel != nil {
		a.sshWorkspaceResolveCancel()
		a.sshWorkspaceResolveCancel = nil
	}
}

func (a *app) closeSSHWorkspacePicker() {
	a.cancelSSHWorkspaceResolve()
	a.closeSSHProfileForm()
	a.sshWorkspaceOpen = false
	a.sshWorkspaceError = ""
}

func (a *app) selectSSHWorkspaceProfile(profileID string) {
	a.cancelSSHWorkspaceResolve()
	for _, profile := range a.settings.SSHProfiles {
		if profile.ID == profileID {
			a.sshBrowser = sshBrowserState{}
			a.sshWorkspaceProfileID = profileID
			a.sshWorkspaceRoot = "/home"
			a.sshWorkspaceError = ""
			a.refreshSSHBrowserOnSelection()
			return
		}
	}
}

func (a *app) selectedSSHProfile() (sshworkspace.Profile, bool) {
	for _, profile := range a.settings.SSHProfiles {
		if profile.ID == a.sshWorkspaceProfileID {
			return profile, true
		}
	}
	return sshworkspace.Profile{}, false
}

func (a *app) openSelectedSSHWorkspace() {
	a.loadSSHFolder(a.sshWorkspaceRoot, true, a.sshBrowser.AllowInit)
}

func (a *app) cancelSSHProfileTest() {
	a.sshProfileTestSeq++
	a.sshProfileTestBusy = false
	if a.sshProfileTestCancel != nil {
		a.sshProfileTestCancel()
		a.sshProfileTestCancel = nil
	}
}

func (a *app) testSSHProfile() {
	if a.sshTestConnection == nil {
		a.sshProfileFormNotice = "SSH connection testing is not wired yet."
		a.sshProfileFormError = ""
		return
	}
	profile, err := a.sshDraftProfile(a.sshProfileDraft)
	if err != nil {
		a.sshProfileFormError = err.Error()
		a.sshProfileFormNotice = ""
		return
	}
	a.cancelSSHProfileTest()
	ctx, cancel := context.WithCancel(context.Background())
	a.sshProfileTestCancel = cancel
	a.sshProfileTestBusy = true
	a.sshProfileTestSeq++
	sequence := a.sshProfileTestSeq
	a.sshProfileFormError = ""
	a.sshProfileFormNotice = "Testing SSH connection…"
	var secrets sshProfileSecrets
	switch profile.AuthMode {
	case sshworkspace.AuthPassword:
		secrets.Password, _, _ = a.sshSecrets().Get(sshworkspace.PasswordSecretRef(profile.ID))
	case sshworkspace.AuthKey:
		secrets.Passphrase, _, _ = a.sshSecrets().Get(sshworkspace.PassphraseSecretRef(profile.ID))
	}
	if a.sshProfileDraft.Password != "" {
		secrets.Password = a.sshProfileDraft.Password
	}
	if a.sshProfileDraft.Passphrase != "" {
		secrets.Passphrase = a.sshProfileDraft.Passphrase
	}
	testConnection := a.sshTestConnection
	go func() {
		defer a.recoverBackground()
		defer cancel()
		err := testConnection(ctx, profile, secrets)
		a.update(func() {
			if sequence != a.sshProfileTestSeq {
				return
			}
			a.sshProfileTestBusy = false
			a.sshProfileTestCancel = nil
			if err == nil {
				a.sshProfileFormNotice = "SSH connection succeeded."
				return
			}
			message := redactSSHProfileError(err, secrets).Error()
			a.sshProfileFormNotice = ""
			a.sshProfileFormError = "SSH connection failed: " + statusErrorLine(message)
		})
	}()
}

func redactSSHProfileError(err error, secrets sshProfileSecrets) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	for _, secret := range []string{secrets.Password, secrets.Passphrase} {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[redacted]")
		}
	}
	return errors.New(message)
}

func validSSHRoot(root string) bool {
	if strings.IndexFunc(root, unicode.IsControl) >= 0 {
		return false
	}
	return root == "" || root == "~" || strings.HasPrefix(root, "~/") || path.IsAbs(root)
}

func sshProfileSummary(profile sshworkspace.Profile) string {
	userHost := profile.Username + "@" + profile.Host
	if profile.Port != 0 && profile.Port != sshworkspace.DefaultPort {
		userHost = fmt.Sprintf("%s:%d", userHost, profile.Port)
	}
	if profile.DefaultRoot != "" {
		return userHost + " · " + profile.DefaultRoot
	}
	return userHost
}
