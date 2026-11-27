package app

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/egoist/mygo/ui"
	"strings"

	"github.com/egoist/mygo"
)

// recentRepository is one repository or plain Editor folder the app opened,
// kept so the appropriate Home tab can offer it again on the next start.
type recentRepository struct {
	Path       string `json:"path"`
	LastOpened int64  `json:"lastOpened"`
	// EditorOnly keeps plain folders on Home's Editor tab, independently of
	// Git repositories. Existing history without this field remains Git history.
	EditorOnly bool `json:"editorOnly,omitempty"`
	// Name is the label the picker shows for this card instead of the
	// folder's name, "" while it has none. It is the user's name for the
	// card alone: the folder, and the repository in it, keep their own.
	Name string `json:"name,omitempty"`
	// Worktree remembers that this checkout is linked to another main
	// worktree, so the homepage can distinguish it without opening it first.
	Worktree bool `json:"worktree,omitempty"`
	// Branch and WorktreeName retain the identity shown beneath the card's
	// path. A detached worktree has no branch, so its own name is the fallback.
	Branch       string `json:"branch,omitempty"`
	WorktreeName string `json:"worktreeName,omitempty"`
	// RepositoryPath identifies the main worktree that owns this linked
	// checkout. It is distinct from Path, which remains the checkout itself.
	RepositoryPath string `json:"repositoryPath,omitempty"`
	// TabColor is the background the repository's tab keeps while open: one of
	// the accent swatches, or a #rrggbb custom color. Empty keeps the theme's
	// own tab background.
	TabColor string `json:"tabColor,omitempty"`
	// PerformanceHintOff records that the user asked not to be reminded about
	// repository performance settings for this repository. It lives in the
	// entry, so removing the entry from the recent list forgets the choice.
	PerformanceHintOff bool `json:"performanceHintOff,omitempty"`
	// Pinned keeps the card at the top of Home's list, whatever the sort, and
	// keeps it from falling off the history's cap.
	Pinned bool `json:"pinned,omitempty"`
}

// recentNameLimit bounds a card's name, in characters, so a long one cannot
// outgrow the card it is drawn on.
const recentNameLimit = 40

// filterRecent returns the original indices of entries whose nickname, folder
// name, or path contains the query. Keeping indices rather than copies is what
// lets a filtered card retain all of the ordinary card actions.
func filterRecent(repositories []recentRepository, query string) []int {
	return filterRecentMode(repositories, query, nil)
}

// filterRecentMode is filterRecent restricted to one Home tab when editorOnly
// is non-nil. Its results remain indices into the shared, persistent history.
func filterRecentMode(repositories []recentRepository, query string, editorOnly *bool) []int {
	query = strings.ToLower(strings.TrimSpace(query))
	visible := make([]int, 0, len(repositories))
	for i, repo := range repositories {
		if editorOnly != nil && repo.EditorOnly != *editorOnly {
			continue
		}
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

// recentTabColorHex returns the remembered tab background for path, if any.
func (a *app) recentTabColorHex(path string) (string, bool) {
	want := cleanRepoPath(path)
	for _, repo := range a.recentRepositories {
		if repo.EditorOnly {
			continue
		}
		if cleanRecentPath(repo.Path) == want && repo.TabColor != "" {
			return repo.TabColor, true
		}
	}
	return "", false
}

// recentTabColor returns the remembered tab background color for path, if any.
func (a *app) recentTabColor(path string) (ui.Color, bool) {
	hex, ok := a.recentTabColorHex(path)
	if !ok {
		return ui.Color{}, false
	}
	color, ok := parseCustomAccent(hex)
	return color, ok
}

// rememberTabColor stores path's tab background in the history, replacing
// whatever it had. Unknown or absent colors keep the remembered one as it is.
func (a *app) rememberTabColor(path, hex string) {
	if _, ok := parseCustomAccent(hex); !ok {
		return
	}
	want := cleanRepoPath(path)
	for i := range a.recentRepositories {
		if a.recentRepositories[i].EditorOnly {
			continue
		}
		if cleanRecentPath(a.recentRepositories[i].Path) == want {
			a.recentRepositories[i].TabColor = hex
			a.historyRevision++
			_ = saveRecentRepositories(a.dataDir(), a.recentRepositories)
			return
		}
	}
}

// setRecentPinned pins or unpins the card for path in the given tab of Home,
// and stores the history.
func (a *app) setRecentPinned(path string, editorOnly, pinned bool) {
	changed := false
	for i := range a.recentRepositories {
		r := &a.recentRepositories[i]
		if r.EditorOnly != editorOnly || r.Path != path || r.Pinned == pinned {
			continue
		}
		r.Pinned = pinned
		changed = true
	}
	if changed {
		a.historyRevision++
		_ = saveRecentRepositories(a.dataDir(), a.recentRepositories)
	}
}

// forgetTabColor clears path's remembered tab background, so the tab returns
// to the theme's own colors.
func (a *app) forgetTabColor(path string) {
	want := cleanRepoPath(path)
	changed := false
	for i := range a.recentRepositories {
		if a.recentRepositories[i].EditorOnly {
			continue
		}
		if cleanRecentPath(a.recentRepositories[i].Path) == want && a.recentRepositories[i].TabColor != "" {
			a.recentRepositories[i].TabColor = ""
			changed = true
		}
	}
	if changed {
		a.historyRevision++
		_ = saveRecentRepositories(a.dataDir(), a.recentRepositories)
	}
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
	return rememberRecentMode(repositories, path, now, false)
}

// rememberRecentMode keeps independent per-tab caps and identities in the
// shared history. The same folder may therefore be a Git and an Editor card,
// while opening one never evicts or reclassifies the other.
func rememberRecentMode(repositories []recentRepository, path string, now int64, editorOnly bool) []recentRepository {
	// Local cards name the checkout itself: make the path absolute and resolve
	// aliases, so a linked worktree never appears under an application's alias.
	path = cleanRepoPath(path)
	if path == "" || path == "." {
		return repositories
	}
	out := make([]recentRepository, 0, len(repositories)+1)
	out = append(out, recentRepository{Path: path, LastOpened: now, EditorOnly: editorOnly})
	kept := 1
	for _, r := range repositories {
		if r.EditorOnly != editorOnly {
			out = append(out, r)
			continue
		}
		if cleanRepoPath(r.Path) == path {
			out[0].Name = r.Name
			out[0].TabColor = r.TabColor
			out[0].PerformanceHintOff = r.PerformanceHintOff
			out[0].Pinned = r.Pinned
			continue
		}
		if kept < maxRecentRepositories || r.Pinned {
			out = append(out, r)
			kept++
		}
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
	if a.recentNameOverrides == nil {
		a.recentNameOverrides = make(map[string]string)
	}
	a.recentNameOverrides[want] = name
	changed := false
	for i := range a.recentRepositories {
		if cleanRecentPath(a.recentRepositories[i].Path) != want ||
			a.recentRepositories[i].Name == name {
			continue
		}
		a.recentRepositories[i].Name = name
		changed = true
	}
	if changed {
		a.historyRevision++
	}
	return changed
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
	if remote, ok := canonicalRemoteReference(path); ok {
		return remote
	}
	return filepath.Clean(strings.TrimSpace(path))
}

type recentHistoryKey struct {
	path       string
	editorOnly bool
}

func recentKey(path string, editorOnly bool) recentHistoryKey {
	return recentHistoryKey{path: cleanRecentPath(path), editorOnly: editorOnly}
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

// recentIdentity is the small amount of Git metadata a recent card needs.
// Reading HEAD and commondir avoids opening every repository just to label it.
type recentIdentity struct {
	worktree       bool
	branch         string
	worktreeName   string
	repositoryPath string
}

// localRecentIdentity recovers cards written before their branch and worktree
// identity was stored. A linked worktree has a .git file whose private Git
// directory names its common directory; a submodule's .git file does not.
func localRecentIdentity(path string) (recentIdentity, bool) {
	if _, remote := canonicalRemoteReference(path); remote {
		return recentIdentity{}, false
	}
	root := filepath.Clean(path)
	dotGit := filepath.Join(root, ".git")
	info, err := os.Stat(dotGit)
	if err != nil {
		return recentIdentity{}, false
	}
	gitDir := dotGit
	identity := recentIdentity{}
	if !info.IsDir() {
		content, err := os.ReadFile(dotGit)
		if err != nil {
			return recentIdentity{}, false
		}
		line, _, _ := strings.Cut(string(content), "\n")
		target, found := strings.CutPrefix(strings.TrimSpace(line), "gitdir:")
		if !found || strings.TrimSpace(target) == "" {
			return recentIdentity{}, false
		}
		target = strings.TrimSpace(target)
		if !filepath.IsAbs(target) {
			target = filepath.Join(root, target)
		}
		gitDir = filepath.Clean(target)
		commonContent, commonErr := os.ReadFile(filepath.Join(gitDir, "commondir"))
		if commonErr == nil && strings.TrimSpace(string(commonContent)) != "" {
			identity.worktree = true
			identity.worktreeName = repoName(root)
			commonGitDir := strings.TrimSpace(string(commonContent))
			if !filepath.IsAbs(commonGitDir) {
				commonGitDir = filepath.Join(gitDir, commonGitDir)
			}
			commonGitDir = filepath.Clean(commonGitDir)
			// In Git's normal non-bare layout the shared directory is the main
			// worktree's .git directory. This recovers the owner without a Git
			// subprocess or a full repository snapshot.
			if filepath.Base(commonGitDir) == ".git" {
				identity.repositoryPath = cleanRepoPath(filepath.Dir(commonGitDir))
			}
		}
	}
	content, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return recentIdentity{}, false
	}
	head := strings.TrimSpace(string(content))
	if head == "" {
		return recentIdentity{}, false
	}
	identity.branch = strings.TrimPrefix(head, "ref: refs/heads/")
	if identity.branch == head {
		identity.branch = ""
	}
	return identity, true
}

// recentIdentityForPath isolates per-card filesystem reads for nonblocking
// history tests. Production always uses localRecentIdentity.
var recentIdentityForPath = localRecentIdentity

// readRecentHistory loads stored cards and recovers their Git identity off the
// UI thread. Canonical path and HEAD/commondir reads may wait on a network or
// removable filesystem; they must never participate in Home rendering.
func readRecentHistory(dir string) ([]recentRepository, bool) {
	repositories := loadRecentRepositories(dir)
	changed := false
	for i := range repositories {
		repo := &repositories[i]
		path := cleanRepoPath(repo.Path)
		if repo.Path != path {
			repo.Path = path
			changed = true
		}
		if repo.EditorOnly {
			continue
		}
		identity, ok := recentIdentityForPath(path)
		if !ok {
			continue
		}
		if repo.Worktree != identity.worktree || repo.Branch != identity.branch ||
			repo.WorktreeName != identity.worktreeName || repo.RepositoryPath != identity.repositoryPath {
			repo.Worktree = identity.worktree
			repo.Branch = identity.branch
			repo.WorktreeName = identity.worktreeName
			repo.RepositoryPath = identity.repositoryPath
			changed = true
		}
	}
	return repositories, changed
}

// mergeRecentHistory preserves changes made while history was loading. An
// existing in-memory card is newer than the delayed file snapshot; unread
// cards are appended without replacing it.
func mergeRecentHistory(current, loaded []recentRepository) []recentRepository {
	out := append([]recentRepository(nil), current...)
	seen := make(map[recentHistoryKey]struct{}, len(current)+len(loaded))
	for _, repo := range current {
		seen[recentKey(repo.Path, repo.EditorOnly)] = struct{}{}
	}
	for _, repo := range loaded {
		key := recentKey(repo.Path, repo.EditorOnly)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, repo)
	}
	return out
}

func (a *app) applyRecentNameOverrides() {
	for i := range a.recentRepositories {
		path := cleanRecentPath(a.recentRepositories[i].Path)
		if name, ok := a.recentNameOverrides[path]; ok {
			a.recentRepositories[i].Name = name
		}
	}
}

// loadHistory starts the stored-history read once the data directory is known.
// The directory can be unavailable before a window exists, so it is called
// again then. Home remains usable with the current in-memory cards while the
// worker runs; the result and any migration write return to the UI thread.
func (a *app) loadHistory() {
	if a.historyLoading || a.historyLoaded {
		return
	}
	dir := a.dataDir()
	if dir == "" {
		return
	}
	a.historyLoading = true
	revision := a.historyRevision
	go func() {
		defer a.recoverBackground()
		repositories, changed := readRecentHistory(dir)
		a.update(func() {
			a.recentRepositories = mergeRecentHistory(a.recentRepositories, repositories)
			a.applyRecentNameOverrides()
			a.historyLoading = false
			a.historyLoaded = true
			if changed && revision == a.historyRevision {
				// Do not let a persistence write undo the responsiveness gained
				// by moving branch reads off the UI thread. A card edited meanwhile
				// has already persisted its own newer state, so skip this migration.
				repositories := append([]recentRepository(nil), a.recentRepositories...)
				go func() {
					defer a.recoverBackground()
					_ = saveRecentRepositories(dir, repositories)
				}()
			}
		})
	}()
}

// rememberOpened keeps a repository the app just opened in the history and
// stores it. Failures are silent: the picker simply starts empty next time.
func (a *app) rememberOpened(path string) {
	before := append([]recentRepository(nil), a.recentRepositories...)
	a.recentRepositories = rememberRecent(a.recentRepositories, path, timeNow())
	want := cleanRepoPath(path)
	worktreeName := ""
	for _, worktree := range a.snapshot.Worktrees {
		if worktree.Current {
			worktreeName = worktree.Name
		}
	}
	linked := a.openedLinkedWorktree()
	repositoryPath := a.openedMainWorktreePath()
	if !linked {
		repositoryPath = ""
	}
	for i := range a.recentRepositories {
		if !a.recentRepositories[i].EditorOnly && cleanRecentPath(a.recentRepositories[i].Path) == want {
			a.recentRepositories[i].Worktree = linked
			a.recentRepositories[i].Branch = a.snapshot.Branch
			a.recentRepositories[i].WorktreeName = worktreeName
			a.recentRepositories[i].RepositoryPath = repositoryPath
			break
		}
	}
	a.historyRevision++
	_ = saveRecentRepositories(a.dataDir(), a.recentRepositories)
	a.cleanupEvictedRecents(before)
}

// rememberOpenedFolder records a plain Editor workspace without inventing Git
// metadata or adding it to Recent repositories.
func (a *app) rememberOpenedFolder(path string) {
	before := append([]recentRepository(nil), a.recentRepositories...)
	a.recentRepositories = rememberRecentMode(a.recentRepositories, path, timeNow(), true)
	a.historyRevision++
	_ = saveRecentRepositories(a.dataDir(), a.recentRepositories)
	a.cleanupEvictedRecents(before)
}

// forgetRecent removes one card from the history, leaving the same path on
// the other Home tab alone. Its conventional GC also removes now-unowned
// cache paths and editor recovery, as the Home card action has always done.
func (a *app) forgetRecent(path string, editorOnly bool) {
	a.forgetRecentWithCachePolicy(path, editorOnly, true)
}

// forgetRecentKeepingRecovery is deliberately separate from the manual Home
// GC: a Cache-page action forgets the card, not unsaved editor contents.
func (a *app) forgetRecentKeepingRecovery(path string, editorOnly bool) {
	a.forgetRecentWithCachePolicy(path, editorOnly, false)
}

func (a *app) forgetRecentWithCachePolicy(path string, editorOnly, reclaimCaches bool) {
	before := append([]recentRepository(nil), a.recentRepositories...)
	kept := make([]recentRepository, 0, len(a.recentRepositories))
	for _, r := range a.recentRepositories {
		if r.Path != path || r.EditorOnly != editorOnly {
			kept = append(kept, r)
		}
	}
	a.recentRepositories = kept
	a.historyRevision++
	_ = saveRecentRepositories(a.dataDir(), a.recentRepositories)
	if reclaimCaches {
		a.cleanupEvictedRecents(before)
	}
}

// recentWorktree reports the identity already remembered for this card. It
// lets a deleted linked checkout fail gently without guessing from its name.
func (a *app) recentWorktree(path string) bool {
	want := cleanRecentPath(path)
	for _, repo := range a.recentRepositories {
		if !repo.EditorOnly && cleanRecentPath(repo.Path) == want {
			return repo.Worktree
		}
	}
	return false
}

func (a *app) pickerOpenPolicy() openPolicy {
	if a.pickerOpenMode == int(openAllowPlainEditor) {
		return openAllowPlainEditor
	}
	return openHomeWithEditorFallback
}

// openRecent fills the Home path field and opens the selected workspace.
func (a *app) openRecent(path string) {
	a.path = path
	a.beginOpenWithPolicy(a.pickerOpenPolicy())
}

// openFromPicker opens what the path field holds, or, while the field is
// empty, the history row the list highlights: Enter and Open then reopen the
// chosen repository instead of complaining about the empty field.
func (a *app) openFromPicker() {
	if strings.TrimSpace(a.path) == "" &&
		a.selectedRecent >= 0 && a.selectedRecent < len(a.recentRepositories) {
		selected := a.recentRepositories[a.selectedRecent]
		if selected.EditorOnly == (a.pickerOpenMode == int(openAllowPlainEditor)) {
			a.openRecent(selected.Path)
			return
		}
	}
	a.beginOpenWithPolicy(a.pickerOpenPolicy())
}

// applyPendingRecent applies a context menu's deferred removal at the top of
// a build pass, before anything reads the list again.
func (a *app) applyPendingRecent() {
	if a.pendingForget != "" {
		a.forgetRecent(a.pendingForget, a.pendingForgetEditorOnly)
		a.pendingForget = ""
		a.pendingForgetEditorOnly = false
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
