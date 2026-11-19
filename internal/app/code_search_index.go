package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	codeSearchIndexVersion                   = 1
	codeSearchIndexManifestFile              = "tangit-index.json"
	codeSearchIndexRefreshInterval           = 30 * time.Second
	codeSearchIndexBackgroundRefreshInterval = 5 * time.Minute
	codeSearchIndexRebuildDelay              = 1500 * time.Millisecond
	codeSearchIndexStagingMaxAge             = 24 * time.Hour
	// codeSearchIndexSourceBytesLimit bounds the source content one repository
	// may feed into Zoekt. Trigram postings can occupy several times the source
	// size on disk; past this budget the complete native search backends are a
	// better trade than silently producing multi-gigabyte cache files.
	codeSearchIndexSourceBytesLimit = 256 << 20 // 256 MiB
)

var errCodeSearchIndexUnavailable = errors.New("indexed search is unavailable on this platform")
var errCodeSearchIndexSourceBudget = fmt.Errorf("repository contents exceed the Zoekt source budget of %s", formatRemoteTransferBytes(codeSearchIndexSourceBytesLimit))

// scheduleCodeSearchIndexRefresh keeps periodic freshness checks off the UI
// thread. Tests can invoke the callback directly instead of waiting 30 seconds.
var scheduleCodeSearchIndexRefresh = func(delay time.Duration, run func()) func() bool {
	timer := time.AfterFunc(delay, run)
	return timer.Stop
}

// scheduleCodeSearchIndexRebuild coalesces file notifications without holding
// up searches. Tests drive this separately from the periodic freshness timer.
var scheduleCodeSearchIndexRebuild = func(delay time.Duration, run func()) {
	time.AfterFunc(delay, run)
}

type codeSearchIndexStatus uint8

const (
	codeSearchIndexMissing codeSearchIndexStatus = iota
	codeSearchIndexChecking
	codeSearchIndexStale
	codeSearchIndexReady
	// codeSearchIndexUnavailable marks a repository too large to index within
	// the storage budget. Search stays on the complete native backends instead
	// of using a partial index; a slower background revalidation can re-enable
	// indexing after the workspace or exclusions shrink enough.
	codeSearchIndexUnavailable
)

type codeSearchIndexState struct {
	status           codeSearchIndexStatus
	directory        string
	requested        bool
	refreshQueued    bool
	refreshScheduled bool
	rebuildScheduled bool
	rebuildSeq       uint64
	refreshCancel    func() bool
	busy             bool
	documents        int
	bytes            int64
	updated          time.Time
	err              string
	token            uint64
	cancel           context.CancelFunc
}

type codeSearchIndexInfo struct {
	status    codeSearchIndexStatus
	documents int
	bytes     int64
	updated   time.Time
}

type codeSearchIndexFile struct {
	Present bool  `json:"present"`
	Size    int64 `json:"size"`
	ModTime int64 `json:"modTime"`
	Indexed bool  `json:"indexed"`
}

type codeSearchIndexManifest struct {
	Version             int                            `json:"version"`
	Root                string                         `json:"root"`
	ExcludedDirectories []string                       `json:"excludedDirectories"`
	Files               map[string]codeSearchIndexFile `json:"files"`
	Documents           int                            `json:"documents"`
	SourceBytes         int64                          `json:"sourceBytes"`
	IndexBytes          int64                          `json:"indexBytes"`
	Updated             time.Time                      `json:"updated"`
}

// The platform adapter owns Zoekt's unstable index API. Keeping construction
// and querying behind this small interface also leaves Windows dependency-free
// at compile time while retaining its existing search engines.
type codeSearchIndexBuilder interface {
	Add(name string, content []byte) error
	Finish() error
}

// Index construction, publication and search share a per-repository lock.
// Searchers hold mapped shard files, so one process must not replace or delete
// a generation under an active query.
var codeSearchIndexLocks sync.Map

func lockCodeSearchIndex(directory string) func() {
	value, _ := codeSearchIndexLocks.LoadOrStore(directory, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

func codeSearchIndexDirectory(cacheDir, root string) string {
	if cacheDir == "" || root == "" {
		return ""
	}
	canonical, err := resolvedCodeSearchIndexRoot(root)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256([]byte(canonical))
	return filepath.Join(cacheDir, "code-search-zoekt", hex.EncodeToString(digest[:16]))
}

func resolvedCodeSearchIndexRoot(root string) (string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(absolute)
}

func (a *app) codeSearchIndexDirectory() string {
	if a.location.IsRemote() || a.code.searchIndex.status != codeSearchIndexReady || a.code.searchIndex.busy || !a.zoektCodeSearchEnabled() {
		return ""
	}
	return a.code.searchIndex.directory
}

func (a *app) resetCodeSearchIndex() {
	if a.code.searchIndex.cancel != nil {
		a.code.searchIndex.cancel()
	}
	if a.code.searchIndex.refreshCancel != nil {
		a.code.searchIndex.refreshCancel()
	}
	token := a.code.searchIndex.token + 1
	a.code.searchIndex = codeSearchIndexState{token: token}
}

func (a *app) codeSearchIndexActiveNow() bool {
	if a.code.searchIndexActive != nil {
		return a.code.searchIndexActive()
	}
	return true
}

func (a *app) codeSearchIndexRefreshDelay() time.Duration {
	if !a.codeSearchIndexActiveNow() || a.code.searchIndex.status == codeSearchIndexUnavailable {
		return codeSearchIndexBackgroundRefreshInterval
	}
	return codeSearchIndexRefreshInterval
}

// resumeCodeSearchIndex immediately revalidates a workspace the user can see
// again, then restores its active polling interval. Inactive tabs do no work.
func (a *app) resumeCodeSearchIndex() {
	if !a.codeSearchIndexActiveNow() || a.location.IsRemote() || a.snapshot.Root == "" {
		return
	}
	state := &a.code.searchIndex
	if state.refreshCancel != nil {
		state.refreshCancel()
		state.refreshCancel = nil
	}
	state.refreshScheduled = false
	state.requested = false
	// A pending debounce must not delay validation after activation.
	state.rebuildScheduled = false
	state.rebuildSeq++
	a.ensureCodeSearchIndex()
}

// ensureCodeSearchIndex validates and, when necessary, rebuilds the local
// index without blocking rendering. Missing, stale, and unsupported indexes all
// leave search free to use its ordinary fallback backends.
func (a *app) ensureCodeSearchIndex() {
	state := &a.code.searchIndex
	if a.location.IsRemote() || a.snapshot.Root == "" || !a.code.filesLoaded || !a.zoektCodeSearchEnabled() {
		return
	}
	if state.status == codeSearchIndexUnavailable {
		// Oversized workspaces never enter the immediate build path again.
		// Occasionally move back to Missing for a stat-only budget recheck;
		// file invalidations still make the workspace retry immediately.
		if state.refreshScheduled {
			return
		}
		state.refreshScheduled = true
		token := state.token
		state.refreshCancel = scheduleCodeSearchIndexRefresh(codeSearchIndexBackgroundRefreshInterval, func() {
			a.updateChanged(func() bool {
				current := &a.code.searchIndex
				if token != current.token || current.status != codeSearchIndexUnavailable {
					return false
				}
				current.refreshScheduled = false
				current.refreshCancel = nil
				if a.codeSearchIndexActiveNow() {
					current.status = codeSearchIndexMissing
					current.requested = false
				}
				a.ensureCodeSearchIndex()
				return false
			})
		})
		return
	}
	if state.directory == "" {
		state.directory = codeSearchIndexDirectory(a.pathCache, a.snapshot.Root)
	}
	if !state.refreshScheduled {
		state.refreshScheduled = true
		token := state.token
		state.refreshCancel = scheduleCodeSearchIndexRefresh(a.codeSearchIndexRefreshDelay(), func() {
			a.updateChanged(func() bool {
				current := &a.code.searchIndex
				if token != current.token {
					// A newer generation owns the schedule slot now.
					return false
				}
				current.refreshScheduled = false
				current.refreshCancel = nil
				// Free the slot even when the workspace went remote or the
				// root is gone, or periodic checks would never re-arm once the
				// workspace is local again. The next local ensure call re-arms.
				if a.location.IsRemote() || a.snapshot.Root == "" {
					return false
				}
				if a.codeSearchIndexActiveNow() {
					current.requested = false
					if current.busy {
						current.refreshQueued = true
					}
				}
				// Active workspaces revalidate now. Inactive ones only re-arm
				// their slower timer; activation or a search validates directly.
				a.ensureCodeSearchIndex()
				return false
			})
		})
	}
	if state.requested || state.busy || state.rebuildScheduled || state.directory == "" || !codeSearchIndexingSupported() {
		return
	}
	state.requested = true
	if state.status != codeSearchIndexReady {
		state.status = codeSearchIndexChecking
	}
	state.busy = true
	state.err = ""
	state.token++
	token, root := state.token, a.snapshot.Root
	// The scheduling closure captured the previous generation: let this one
	// replace it without leaving a stale timer able to retire the new request.
	state.refreshScheduled = false
	directory, paths := state.directory, append([]string(nil), a.code.files...)
	excluded := append([]string(nil), a.settings.SearchExcludedDirectories...)
	ctx, cancel := context.WithCancel(context.Background())
	state.cancel = cancel
	go func() {
		defer a.recoverBackground()
		unlock := lockCodeSearchIndex(directory)
		info, err := inspectCodeSearchIndex(ctx, root, paths, excluded, directory)
		unlock()
		if info.status == codeSearchIndexUnavailable && errors.Is(err, errCodeSearchIndexSourceBudget) {
			// Indexes published by older releases may exceed the new budget.
			// Reclaim them under the build lock instead of leaving several GiB
			// on disk merely because the repository is still in recents.
			removeCodeSearchIndexDirectory(directory)
			info.bytes = 0
		}
		rebuilt := false
		if ctx.Err() == nil && (info.status == codeSearchIndexMissing || info.status == codeSearchIndexStale) {
			info, err = buildCodeSearchIndex(ctx, root, paths, excluded, directory)
			rebuilt = err == nil && info.status == codeSearchIndexReady
		}
		a.updateChanged(func() bool {
			current := &a.code.searchIndex
			if token != current.token || root != a.snapshot.Root {
				return false
			}
			current.busy = false
			current.cancel = nil
			if ctx.Err() != nil {
				current.requested = false
				current.refreshQueued = false
				// A newer file notification normally owns a debounce timer. If
				// cancellation came from elsewhere, respect that gate as well.
				a.ensureCodeSearchIndex()
				return false
			}
			previous := codeSearchIndexInfo{
				status: current.status, documents: current.documents,
				bytes: current.bytes, updated: current.updated,
			}
			previousErr := current.err
			current.status = info.status
			current.documents = info.documents
			current.bytes = info.bytes
			current.updated = info.updated
			current.err = safeError(err)
			visibleChanged := previous != (codeSearchIndexInfo{
				status: current.status, documents: current.documents,
				bytes: current.bytes, updated: current.updated,
			}) || previousErr != current.err
			queued := current.refreshQueued
			current.refreshQueued = false
			if queued {
				current.requested = false
			}
			a.ensureCodeSearchIndex()
			if rebuilt && a.code.searchSubmitted != "" && !a.code.searchReplaceRunning {
				a.beginCodeSearch()
			}
			return visibleChanged && a.workspaceTab == sectionCode
		})
	}()
}

// invalidateCodeSearchIndex marks the old generation unusable immediately.
// A burst of changes cancels obsolete staging work and shares one delayed
// rebuild; searches meanwhile use ripgrep or the native scanner without
// waiting for the old build to finish.
func (a *app) invalidateCodeSearchIndex() {
	if a.location.IsRemote() || a.snapshot.Root == "" {
		return
	}
	state := &a.code.searchIndex
	if state.status != codeSearchIndexMissing {
		state.status = codeSearchIndexStale
	}
	state.requested = false
	state.refreshQueued = false
	if state.cancel != nil {
		state.cancel()
	}
	state.rebuildScheduled = true
	state.rebuildSeq++
	sequence, token := state.rebuildSeq, state.token
	scheduleCodeSearchIndexRebuild(codeSearchIndexRebuildDelay, func() {
		a.updateChanged(func() bool {
			current := &a.code.searchIndex
			if token != current.token || sequence != current.rebuildSeq {
				return false
			}
			current.rebuildScheduled = false
			if a.location.IsRemote() || a.snapshot.Root == "" || !a.codeSearchIndexActiveNow() {
				return false
			}
			current.requested = false
			a.ensureCodeSearchIndex()
			return a.workspaceTab == sectionCode
		})
	})
}

// buildCodeSearchIndex stages a complete generation beside the published one,
// then swaps it into place. An interrupted build never exposes a partial index.
func buildCodeSearchIndex(ctx context.Context, root string, paths, excluded []string, directory string) (codeSearchIndexInfo, error) {
	missing := codeSearchIndexInfo{status: codeSearchIndexMissing}
	if directory == "" {
		return missing, fmt.Errorf("search index directory is empty")
	}
	if !codeSearchIndexingSupported() {
		return missing, errCodeSearchIndexUnavailable
	}
	unlock := lockCodeSearchIndex(directory)
	defer unlock()
	if err := ctx.Err(); err != nil {
		return missing, err
	}
	resolvedRoot, err := resolvedCodeSearchIndexRoot(root)
	if err != nil {
		return missing, err
	}
	overBudget, err := exceedsCodeSearchIndexSourceBudget(ctx, resolvedRoot, paths, excluded)
	if err != nil {
		return missing, err
	}
	if overBudget {
		// Deleting an older generation is safe here: the per-index lock keeps
		// builders serialized and searchers use this same lock before mapping.
		// Never replace a complete index with a deliberately partial one.
		if err = os.RemoveAll(directory); err != nil {
			return missing, err
		}
		return codeSearchIndexInfo{status: codeSearchIndexUnavailable, updated: time.Now().UTC()}, errCodeSearchIndexSourceBudget
	}
	parent := filepath.Dir(directory)
	if err = os.MkdirAll(parent, 0o700); err != nil {
		return missing, err
	}
	cleanupCodeSearchIndexStaging(parent, time.Now())
	staging, err := os.MkdirTemp(parent, ".tangit-index-")
	if err != nil {
		return missing, err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(staging)
		}
	}()

	builder, err := newCodeSearchIndexBuilder(staging)
	if err != nil {
		return missing, err
	}
	finished := false
	defer func() {
		if !finished {
			_ = builder.Finish()
		}
	}()
	manifest := codeSearchIndexManifest{
		Version: codeSearchIndexVersion, Root: resolvedRoot,
		ExcludedDirectories: normalizeCodeSearchIndexExclusions(excluded),
		Files:               make(map[string]codeSearchIndexFile), Updated: time.Now().UTC(),
	}
	filter := workspaceSearchOptions{ExcludedDirectories: excluded}
	for _, candidate := range paths {
		if err = ctx.Err(); err != nil {
			return missing, err
		}
		if !filter.pathIncluded(candidate) {
			continue
		}
		cleaned, ok := cleanWorkspaceSearchPath(candidate)
		if !ok {
			continue
		}
		if _, duplicate := manifest.Files[cleaned]; duplicate {
			return missing, fmt.Errorf("duplicate search path %q", cleaned)
		}
		filename, ok := resolveWorkspaceSearchPath(resolvedRoot, cleaned)
		if !ok {
			manifest.Files[cleaned] = codeSearchIndexFile{}
			continue
		}
		before, statErr := os.Stat(filename)
		if statErr != nil || !before.Mode().IsRegular() {
			manifest.Files[cleaned] = codeSearchIndexFile{}
			continue
		}
		entry := codeSearchIndexFile{Present: true, Size: before.Size(), ModTime: before.ModTime().UnixNano()}
		data, readOK := workspaceSearchReadFile(ctx, filename)
		after, afterErr := os.Stat(filename)
		stable := afterErr == nil && after.Mode().IsRegular() && after.Size() == entry.Size && after.ModTime().UnixNano() == entry.ModTime
		if readOK && stable && ctx.Err() == nil {
			// Zoekt's high-level Builder writes NOT-INDEXED for files shorter
			// than three bytes. Keep those (and any rejected file) in the small
			// native complement instead of accepting silent false negatives.
			entry.Indexed = len(data) >= 3
			if entry.Indexed {
				if err = builder.Add(cleaned, data); err != nil {
					return missing, err
				}
				manifest.Documents++
			}
			manifest.SourceBytes += int64(len(data))
		}
		manifest.Files[cleaned] = entry
	}
	if err = ctx.Err(); err != nil {
		return missing, err
	}
	finished = true
	if err = builder.Finish(); err != nil {
		return missing, err
	}
	manifest.IndexBytes = codeSearchIndexDiskBytes(staging)
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return missing, err
	}
	if err = os.WriteFile(filepath.Join(staging, codeSearchIndexManifestFile), encoded, 0o600); err != nil {
		return missing, err
	}
	if err = ctx.Err(); err != nil {
		return missing, err
	}
	backup := staging + "-previous"
	if err = os.Rename(directory, backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return missing, err
	}
	if err = os.Rename(staging, directory); err != nil {
		_ = os.Rename(backup, directory)
		return missing, err
	}
	published = true
	_ = os.RemoveAll(backup)
	return codeSearchIndexInfo{
		status: codeSearchIndexReady, documents: manifest.Documents,
		bytes: manifest.IndexBytes, updated: manifest.Updated,
	}, nil
}

// cleanupCodeSearchIndexStaging removes only old temporary generations. The
// age floor protects a builder in another process, while the per-index lock
// prevents this from racing a builder in this process.
func cleanupCodeSearchIndexStaging(parent string, now time.Time) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	cutoff := now.Add(-codeSearchIndexStagingMaxAge)
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), ".tangit-index-") {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(parent, entry.Name()))
	}
}

func normalizeCodeSearchIndexExclusions(excluded []string) []string {
	return normalizeWorkspaceSearchExcludedDirectories(excluded)
}

// exceedsCodeSearchIndexSourceBudget estimates storage before any source file
// is read or shard is written. Stat-only preflight is cheap compared with a
// multi-GiB build. Counting regular file sizes is deliberately conservative:
// binary files rejected later do not weaken the build-time storage bound.
func exceedsCodeSearchIndexSourceBudget(ctx context.Context, resolvedRoot string, paths, excluded []string) (bool, error) {
	filter := workspaceSearchOptions{ExcludedDirectories: excluded}
	var sourceBytes int64
	for _, candidate := range paths {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if !filter.pathIncluded(candidate) {
			continue
		}
		cleaned, ok := cleanWorkspaceSearchPath(candidate)
		if !ok {
			continue
		}
		filename, ok := resolveWorkspaceSearchPath(resolvedRoot, cleaned)
		if !ok {
			continue
		}
		info, err := os.Stat(filename)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if info.Size() > codeSearchIndexSourceBytesLimit-sourceBytes {
			return true, nil
		}
		sourceBytes += info.Size()
	}
	return false, nil
}

func codeSearchIndexDiskBytes(directory string) int64 {
	var total int64
	_ = filepath.WalkDir(directory, func(_ string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

func loadCodeSearchIndexManifest(directory string) (codeSearchIndexManifest, error) {
	data, err := os.ReadFile(filepath.Join(directory, codeSearchIndexManifestFile))
	if err != nil {
		return codeSearchIndexManifest{}, err
	}
	var manifest codeSearchIndexManifest
	if err = json.Unmarshal(data, &manifest); err != nil {
		return codeSearchIndexManifest{}, err
	}
	return manifest, nil
}

func inspectCodeSearchIndex(ctx context.Context, root string, paths, excluded []string, directory string) (codeSearchIndexInfo, error) {
	manifest, err := loadCodeSearchIndexManifest(directory)
	if errors.Is(err, os.ErrNotExist) {
		return codeSearchIndexInfo{status: codeSearchIndexMissing}, nil
	}
	if err != nil {
		return codeSearchIndexInfo{status: codeSearchIndexStale}, err
	}
	stale := codeSearchIndexInfo{
		status: codeSearchIndexStale, documents: manifest.Documents,
		bytes: manifest.IndexBytes, updated: manifest.Updated,
	}
	if manifest.Version != codeSearchIndexVersion {
		return stale, nil
	}
	resolvedRoot, err := resolvedCodeSearchIndexRoot(root)
	if err != nil || resolvedRoot != manifest.Root ||
		!slices.Equal(normalizeCodeSearchIndexExclusions(excluded), manifest.ExcludedDirectories) {
		return stale, nil
	}
	if manifest.SourceBytes > codeSearchIndexSourceBytesLimit {
		overBudget, checkErr := exceedsCodeSearchIndexSourceBudget(ctx, resolvedRoot, paths, excluded)
		if checkErr != nil {
			return stale, checkErr
		}
		if overBudget {
			return codeSearchIndexInfo{
				status: codeSearchIndexUnavailable, documents: manifest.Documents, updated: manifest.Updated,
			}, errCodeSearchIndexSourceBudget
		}
		// Files or exclusions have since brought the repository back under
		// budget. Replace the old oversized generation with a bounded one.
		return stale, nil
	}
	if fresh, checkErr := codeSearchIndexFresh(ctx, resolvedRoot, paths, excluded, manifest); checkErr != nil {
		return stale, checkErr
	} else if !fresh {
		return stale, nil
	}
	return codeSearchIndexInfo{
		status: codeSearchIndexReady, documents: manifest.Documents,
		bytes: manifest.IndexBytes, updated: manifest.Updated,
	}, nil
}

func codeSearchIndexFresh(ctx context.Context, resolvedRoot string, paths, excluded []string, manifest codeSearchIndexManifest) (bool, error) {
	filter := workspaceSearchOptions{ExcludedDirectories: excluded}
	seen := make(map[string]bool, len(manifest.Files))
	for _, candidate := range paths {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if !filter.pathIncluded(candidate) {
			continue
		}
		cleaned, ok := cleanWorkspaceSearchPath(candidate)
		if !ok {
			continue
		}
		entry, indexed := manifest.Files[cleaned]
		if !indexed || seen[cleaned] {
			return false, nil
		}
		seen[cleaned] = true
		filename, ok := resolveWorkspaceSearchPath(resolvedRoot, cleaned)
		if !ok {
			if entry.Present {
				return false, nil
			}
			continue
		}
		info, err := os.Stat(filename)
		if err != nil || !info.Mode().IsRegular() {
			if entry.Present {
				return false, nil
			}
			continue
		}
		if !entry.Present || entry.Size != info.Size() || entry.ModTime != info.ModTime().UnixNano() {
			return false, nil
		}
	}
	return len(seen) == len(manifest.Files), nil
}

// searchWorkspaceFilesZoekt uses Zoekt only to discover candidate files. The
// ordinary native matcher then reads those files afresh, preserving all search
// semantics and removing any match that changed after the index was built.
func searchWorkspaceFilesZoekt(ctx context.Context, root string, paths []string, options workspaceSearchOptions, limit int) ([]workspaceSearchMatch, bool) {
	if options.IndexDirectory == "" || !codeSearchIndexingSupported() {
		return nil, false
	}
	unlock := lockCodeSearchIndex(options.IndexDirectory)
	defer unlock()
	info, err := inspectCodeSearchIndex(ctx, root, paths, options.ExcludedDirectories, options.IndexDirectory)
	if err != nil || info.status != codeSearchIndexReady {
		return nil, false
	}
	candidates, err := codeSearchIndexCandidates(ctx, options.IndexDirectory, len(paths), options)
	if err != nil || ctx.Err() != nil {
		return nil, false
	}
	manifest, err := loadCodeSearchIndexManifest(options.IndexDirectory)
	if err != nil {
		return nil, false
	}
	ordered := make([]string, 0, len(candidates))
	for _, candidate := range paths {
		cleaned, ok := cleanWorkspaceSearchPath(candidate)
		if !ok {
			continue
		}
		entry := manifest.Files[cleaned]
		// The native complement covers short files and any file Zoekt could
		// not index. Ordinary filtering still runs in the shared Go matcher.
		if candidates[cleaned] || (entry.Present && !entry.Indexed) {
			ordered = append(ordered, candidate)
		}
	}
	matches, matchErr := searchWorkspaceFilesGoWithOptions(ctx, root, ordered, options, limit)
	if matchErr != nil || ctx.Err() != nil {
		return nil, false
	}
	// A source change during the query invalidates the candidate snapshot;
	// fall back rather than missing a newly introduced match in an old file.
	resolvedRoot, err := resolvedCodeSearchIndexRoot(root)
	if err != nil {
		return nil, false
	}
	fresh, err := codeSearchIndexFresh(ctx, resolvedRoot, paths, options.ExcludedDirectories, manifest)
	if err != nil || !fresh {
		return nil, false
	}
	return matches, true
}
