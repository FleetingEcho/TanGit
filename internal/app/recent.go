package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
)

// recentRepository is one repository the app opened, kept so the picker can
// offer it again on the next start.
type recentRepository struct {
	Path       string `json:"path"`
	LastOpened int64  `json:"lastOpened"`
	// Name is the label the picker shows for this card instead of the
	// folder's name, "" while it has none. It is the user's name for the
	// card alone: the folder, and the repository in it, keep their own.
	Name string `json:"name,omitempty"`
}

// recentNameLimit bounds a card's name, in characters, so a long one cannot
// outgrow the card it is drawn on.
const recentNameLimit = 40

// filterRecent returns the original indices of entries whose nickname, folder
// name, or path contains the query. Keeping indices rather than copies is what
// lets a filtered card retain all of the ordinary card actions.
func filterRecent(repositories []recentRepository, query string) []int {
	query = strings.ToLower(strings.TrimSpace(query))
	visible := make([]int, 0, len(repositories))
	for i, repo := range repositories {
		if query == "" || strings.Contains(strings.ToLower(repo.displayName()), query) ||
			strings.Contains(strings.ToLower(repo.Path), query) {
			visible = append(visible, i)
		}
	}
	return visible
}

// displayName is the card's title: the name given to the entry, or the
// folder's own while it has none.
func (r recentRepository) displayName() string {
	if name := strings.TrimSpace(r.Name); name != "" {
		return name
	}
	return repoName(r.Path)
}

// renamed reports whether the card carries a name of its own, rather than the
// folder's.
func (r recentRepository) renamed() bool {
	name := strings.TrimSpace(r.Name)
	return name != "" && name != repoName(r.Path)
}

// maxRecentRepositories caps the history; the oldest falls off beyond it.
const maxRecentRepositories = 12

// recentRepositoriesFile is the name of the history inside MyGo's user data
// directory (~/Library/Application Support, $XDG_CONFIG_HOME or %AppData%,
// joined with the application name).
const recentRepositoriesFile = "recent-repositories.json"

// recentStorePath names the history file inside dir, "" when there is no
// directory to store it in.
func recentStorePath(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, recentRepositoriesFile)
}

// loadRecentRepository reads the history stored in the directory dir. It
// never fails the app: a missing or damaged file gives an empty history.
func loadRecentRepositories(dir string) []recentRepository {
	path := recentStorePath(dir)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var repositories []recentRepository
	if err := json.Unmarshal(data, &repositories); err != nil {
		return nil
	}
	return repositories
}

// saveRecentRepositories writes the history to the directory dir, creating it
// when needed. The write is atomic: a crash mid-write leaves the last good
// file rather than a truncated one.
func saveRecentRepositories(dir string, repositories []recentRepository) error {
	path := recentStorePath(dir)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(repositories, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// rememberRecent puts path first in the history at now (Unix time), dropping
// a second entry for it and, past the cap, the entry opened longest ago. The
// name given to the entry stays with it, because it belongs to the repository
// rather than to one opening of it. It copies, so the caller's slice is never
// shared with the app's.
func rememberRecent(repositories []recentRepository, path string, now int64) []recentRepository {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return repositories
	}
	out := make([]recentRepository, 0, len(repositories)+1)
	out = append(out, recentRepository{Path: path, LastOpened: now})
	for _, r := range repositories {
		if r.Path == path {
			out[0].Name = r.Name
			continue
		}
		out = append(out, r)
	}
	if len(out) > maxRecentRepositories {
		out = out[:maxRecentRepositories]
	}
	return out
}

// recentNameForStorage turns what the field holds into the name an entry
// keeps: an empty name, or the folder's own, clears the name, and a long one
// stops at what a card or a pill can show.
func recentNameForStorage(path, name string) string {
	name = strings.TrimSpace(name)
	if name == repoName(path) {
		return ""
	}
	if runes := []rune(name); len(runes) > recentNameLimit {
		name = strings.TrimSpace(string(runes[:recentNameLimit]))
	}
	return name
}

// renameRecentEntry gives the entry for path the name, and stores the history.
// An empty name, or the folder's own, leaves the card showing the folder name.
// It reports whether anything changed.
func (a *app) renameRecentEntry(path, name string) bool {
	if !a.setRecentName(path, recentNameForStorage(path, name)) {
		return false
	}
	_ = saveRecentRepositories(a.dataDir(), a.recentRepositories)
	return true
}

// setRecentName changes the name the in-memory entry for path holds, without
// writing the history, so a name can reach every page of the window before
// one of them stores it.
func (a *app) setRecentName(path, name string) bool {
	want := cleanRecentPath(path)
	for i := range a.recentRepositories {
		if cleanRecentPath(a.recentRepositories[i].Path) != want ||
			a.recentRepositories[i].Name == name {
			continue
		}
		a.recentRepositories[i].Name = name
		return true
	}
	return false
}

// recentNameFor is the name the user gave the repository at path, "" while it
// has none. A page's pill takes it as its title, so the name a tab was given
// survives the frame that saved it.
func (a *app) recentNameFor(path string) string {
	want := cleanRecentPath(path)
	for _, r := range a.recentRepositories {
		if cleanRecentPath(r.Path) == want {
			return strings.TrimSpace(r.Name)
		}
	}
	return ""
}

// cleanRecentPath compares two ways of writing the same folder, without
// touching the file system: the entry keeps the path it was stored with.
func cleanRecentPath(path string) string {
	return filepath.Clean(strings.TrimSpace(path))
}

// openRecentRename opens the modal that names the entry for path. The field
// starts with the name the entry already has, so a mistyped one can be fixed,
// and an empty field leaves the folder's name showing.
func (a *app) openRecentRename(path string) {
	a.renameRecentFromTab = false
	a.startRenameRecent(path)
}

// openTabRename is openRecentRename for the window's tab menu, which asks for
// the page's name rather than the card's. They are one name.
func (a *app) openTabRename(path string) {
	a.renameRecentFromTab = true
	a.startRenameRecent(path)
}

func (a *app) startRenameRecent(path string) {
	a.renameRecentPath = path
	a.renameRecentName = ""
	want := cleanRecentPath(path)
	for _, r := range a.recentRepositories {
		if cleanRecentPath(r.Path) == want {
			a.renameRecentName = r.Name
		}
	}
	a.showRenameRecent = true
}

// beginRenameRecent applies the name the modal holds. The list is rewritten
// on the next build pass, as a removal is: the frame is reading it now.
func (a *app) beginRenameRecent() {
	path := a.renameRecentPath
	a.renameRecentFromTab = false
	if path == "" {
		a.showRenameRecent = false
		return
	}
	a.pendingRenamePath = path
	a.pendingRenameName = strings.TrimSpace(a.renameRecentName)
	a.pendingRename = true
	a.showRenameRecent = false
}

// dataDir is the per-user directory the app persists in, "" when unavailable
// (before a window exists, or with no user config directory).
func (a *app) dataDir() string {
	if a.pathUserData != "" {
		return a.pathUserData
	}
	if a.window != nil {
		if dir, err := mygo.App.Path(mygo.PathUserData); err == nil {
			a.pathUserData = dir
		}
	}
	return a.pathUserData
}

// loadHistory reads the stored history once the data directory is known. The
// directory can be unavailable before a window exists, so it is called again
// then; historyLoaded keeps the file from being re-read on every frame.
func (a *app) loadHistory() {
	if a.historyLoaded {
		return
	}
	dir := a.dataDir()
	if dir == "" {
		return
	}
	a.recentRepositories = loadRecentRepositories(dir)
	a.historyLoaded = true
}

// rememberOpened keeps a repository the app just opened in the history and
// stores it. Failures are silent: the picker simply starts empty next time.
func (a *app) rememberOpened(path string) {
	a.recentRepositories = rememberRecent(a.recentRepositories, path, timeNow())
	_ = saveRecentRepositories(a.dataDir(), a.recentRepositories)
}

// forgetRecent removes the repository path from the history.
func (a *app) forgetRecent(path string) {
	kept := make([]recentRepository, 0, len(a.recentRepositories))
	for _, r := range a.recentRepositories {
		if r.Path != path {
			kept = append(kept, r)
		}
	}
	a.recentRepositories = kept
	_ = saveRecentRepositories(a.dataDir(), a.recentRepositories)
}

// openRecent fills the path field with the repository and opens it through
// the same path beginOpen takes.
func (a *app) openRecent(path string) {
	a.path = path
	a.beginOpen()
}

// openFromPicker opens what the path field holds, or, while the field is
// empty, the history row the list highlights: Enter and Open then reopen the
// chosen repository instead of complaining about the empty field.
func (a *app) openFromPicker() {
	if strings.TrimSpace(a.path) == "" &&
		a.selectedRecent >= 0 && a.selectedRecent < len(a.recentRepositories) {
		a.openRecent(a.recentRepositories[a.selectedRecent].Path)
		return
	}
	a.beginOpen()
}

// applyPendingRecent applies a context menu's deferred removal at the top of
// a build pass, before anything reads the list again.
func (a *app) applyPendingRecent() {
	if a.pendingForget != "" {
		a.forgetRecent(a.pendingForget)
		a.pendingForget = ""
		if a.selectedRecent >= len(a.recentRepositories) {
			a.selectedRecent = len(a.recentRepositories) - 1
		}
	}
	if a.pendingRename {
		path := a.pendingRenamePath
		name := a.pendingRenameName
		a.renameRecentEntry(path, name)
		if a.onRecentRenamed != nil {
			a.onRecentRenamed(path, recentNameForStorage(path, name))
		}
		a.pendingRenamePath = ""
		a.pendingRenameName = ""
		a.pendingRename = false
	}
}
