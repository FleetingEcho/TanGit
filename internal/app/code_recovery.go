package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui/editor"
)

const codeRecoveryVersion = 2

type codeRecoveryEntry struct {
	Path    string         `json:"path"`
	Stamp   localFileStamp `json:"stamp"`
	Content []byte         `json:"content"`
}

type codeRecoveryFile struct {
	Version int                 `json:"version"`
	Root    string              `json:"root"`
	Entries []codeRecoveryEntry `json:"entries"`
}

func codeRecoveryPath(dataDir, root string) string {
	if dataDir == "" || root == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(filepath.Clean(root)))
	return filepath.Join(dataDir, "editor-recovery", hex.EncodeToString(digest[:])+".json")
}

func readCodeRecovery(filename, root string) (codeRecoveryFile, error) {
	content, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		return codeRecoveryFile{}, nil
	}
	if err != nil {
		return codeRecoveryFile{}, err
	}
	var recovery codeRecoveryFile
	if err := json.Unmarshal(content, &recovery); err != nil {
		return codeRecoveryFile{}, err
	}
	if recovery.Version != codeRecoveryVersion || filepath.Clean(recovery.Root) != filepath.Clean(root) {
		return codeRecoveryFile{}, errors.New("editor recovery belongs to a different workspace or version")
	}
	return recovery, nil
}

func writeCodeRecovery(filename string, recovery codeRecoveryFile) error {
	if len(recovery.Entries) == 0 {
		if err := os.Remove(filename); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		return err
	}
	content, err := json.Marshal(recovery)
	if err != nil {
		return err
	}
	return mygo.WriteFileSafely(filename, func(w io.Writer) error {
		_, err := w.Write(content)
		return err
	})
}

// syncCodeRecovery snapshots dirty buffers after a short quiet period. The
// recovery file contains app-owned copies only; restoring never writes the
// workspace and only proceeds while the on-disk bytes still match the base.
func (a *app) syncCodeRecovery() {
	root := a.snapshot.Root
	filename := codeRecoveryPath(a.dataDir(), root)
	if a.location.IsRemote() || filename == "" {
		return
	}
	if a.code.recoveryRoot != root {
		a.code.recoveryRoot = root
		a.code.recoveryLoaded = false
		a.code.recoverySeq++
		sequence := a.code.recoverySeq
		generation := a.generation
		go func() {
			defer a.recoverBackground()
			recovery, err := readCodeRecovery(filename, root)
			a.updateChanged(func() bool {
				if generation != a.generation || root != a.snapshot.Root || sequence != a.code.recoverySeq {
					return false
				}
				if err != nil {
					a.notify("warning", tr("Could not read editor recovery"), safeError(err))
					return true
				}
				a.code.recoveryLoaded = true
				a.code.recoveryEntries = make(map[string]codeRecoveryEntry, len(recovery.Entries))
				for _, entry := range recovery.Entries {
					if !validCodeManagedPath(entry.Path) {
						continue
					}
					a.code.recoveryEntries[entry.Path] = entry
					a.openPinnedCodeFile(entry.Path, 0)
				}
				return len(recovery.Entries) > 0
			})
		}()
		return
	}
	if !a.code.recoveryLoaded {
		return
	}
	entries := make([]codeRecoveryEntry, 0, len(a.code.documents))
	seen := make(map[string]bool, len(a.code.documents))
	for _, document := range a.code.documents {
		if !codeDocumentModified(document) || !document.localStampKnown {
			continue
		}
		entries = append(entries, codeRecoveryEntry{Path: document.path, Stamp: document.localStamp, Content: []byte(document.state.Buffer.String())})
		seen[document.path] = true
	}
	// Keep entries whose files are still loading. They are removed from this
	// map only after restoreCodeRecoveryEntry has restored or rejected them.
	for path, entry := range a.code.recoveryEntries {
		if !seen[path] {
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	recovery := codeRecoveryFile{Version: codeRecoveryVersion, Root: root, Entries: entries}
	content, _ := json.Marshal(recovery)
	signature := stampLocalFile(content)
	if signature == a.code.recoverySignature {
		return
	}
	a.code.recoverySignature = signature
	a.code.recoverySeq++
	sequence := a.code.recoverySeq
	generation := a.generation
	time.AfterFunc(750*time.Millisecond, func() {
		a.updateChanged(func() bool {
			if generation != a.generation || sequence != a.code.recoverySeq {
				return false
			}
			go func() {
				defer a.recoverBackground()
				if err := writeCodeRecovery(filename, recovery); err != nil {
					a.updateChanged(func() bool {
						if generation != a.generation || sequence != a.code.recoverySeq {
							return false
						}
						a.code.recoverySignature = localFileStamp{}
						a.notify("warning", tr("Could not save editor recovery"), safeError(err))
						return true
					})
				}
			}()
			return false
		})
	})
}

func (a *app) restoreCodeRecoveryEntry(document *codeDocument) {
	if document == nil || document.state == nil || document.state.Buffer == nil {
		return
	}
	entry, ok := a.code.recoveryEntries[document.path]
	if !ok {
		return
	}
	delete(a.code.recoveryEntries, document.path)
	if !document.localStampKnown || entry.Stamp != document.localStamp {
		a.notify("warning", tr("Editor recovery skipped"), trf("%s changed on disk", document.path))
		return
	}
	original := document.state.Buffer.String()
	if string(entry.Content) == original {
		return
	}
	document.state.Buffer.Edit(editor.Edit{Start: 0, End: len(original), Text: string(entry.Content)})
	a.notify("success", tr("Unsaved editor changes restored"), document.path)
}
