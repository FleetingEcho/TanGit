package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jesseduffield/lazygit/pkg/commands/models"
)

// reviewFile is deliberately tiny and non-secret: a path's mark is only the
// digest of content Git already holds or the working tree already contains.
// It lives in this worktree's Git directory, never among the user's changes.
type reviewFile struct {
	Version int               `json:"version"`
	Files   map[string]string `json:"files"`
}

func (s *gitSession) reviewPath() string {
	return filepath.Join(s.git.RepoPaths.WorktreeGitDirPath(), "tangit", "reviewed.json")
}

func readReviewFile(name string) (reviewFile, error) {
	state := reviewFile{Version: 1, Files: map[string]string{}}
	data, err := os.ReadFile(name)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return reviewFile{}, fmt.Errorf("read review state: %w", err)
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return reviewFile{}, fmt.Errorf("read review state: %w", err)
	}
	if state.Version != 1 {
		return reviewFile{}, fmt.Errorf("read review state: unsupported version %d", state.Version)
	}
	if state.Files == nil {
		state.Files = map[string]string{}
	}
	return state, nil
}

func writeReviewFile(name string, state reviewFile) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		return fmt.Errorf("create review-state folder: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(name), "reviewed-*.json")
	if err != nil {
		return fmt.Errorf("write review state: %w", err)
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return fmt.Errorf("protect review state: %w", err)
	}
	if err := json.NewEncoder(file).Encode(state); err != nil {
		file.Close()
		return fmt.Errorf("write review state: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("write review state: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("write review state: %w", err)
	}
	if err := os.Rename(temporary, name); err != nil {
		return fmt.Errorf("replace review state: %w", err)
	}
	return nil
}

// reviewFingerprint binds a mark to both sides of the review, not to whether
// the same bytes currently happen to be staged. The patch carries HEAD's side
// and renames/modes; the explicit content read also covers untracked, unmerged,
// empty and binary files. Git's empty tree is used only for an unborn HEAD.
func (s *gitSession) reviewFingerprint(ctx context.Context, file File) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	base := models.EmptyTreeCommitHash
	if s.cmd.New([]string{"git", "rev-parse", "--verify", "HEAD"}).Run() == nil {
		base = "HEAD"
	}
	args := []string{"git", "diff", "--no-color", "--no-ext-diff", "--no-textconv", "--binary", "--full-index", base, "--"}
	args = append(args, fileNames(file)...)
	patch, err := s.cmd.New(args).RunWithOutput()
	if err != nil {
		return "", fmt.Errorf("read review identity for %s: %w: %s", file.Path, err, patch)
	}
	hash := sha256.New()
	hash.Write([]byte(patch))
	hash.Write([]byte{0})
	fmt.Fprintf(hash, "tracked=%v deleted=%v previous=%s\x00", file.Tracked, file.Deleted, file.PreviousPath)
	path, err := worktreeFilePath(s.git.RepoPaths.WorktreePath(), file.Path)
	if err != nil {
		return "", err
	}
	info, statErr := os.Lstat(path)
	switch {
	case os.IsNotExist(statErr):
		hash.Write([]byte("absent"))
	case statErr != nil:
		return "", fmt.Errorf("read review content for %s: %w", file.Path, statErr)
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return "", fmt.Errorf("read review link for %s: %w", file.Path, err)
		}
		hash.Write([]byte("symlink\x00" + target))
	case info.Mode().IsRegular():
		content, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read review content for %s: %w", file.Path, err)
		}
		hash.Write([]byte("file\x00"))
		hash.Write(content)
	default:
		fmt.Fprintf(hash, "mode=%s", info.Mode())
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *gitSession) ReviewedFiles(ctx context.Context, files []File) (map[string]bool, error) {
	s.reviewMu.Lock()
	defer s.reviewMu.Unlock()
	state, err := readReviewFile(s.reviewPath())
	if err != nil {
		return nil, err
	}
	reviewed := make(map[string]bool, len(files))
	for _, file := range files {
		digest, marked := state.Files[file.Path]
		if !marked {
			continue
		}
		identity, err := s.reviewFingerprint(ctx, file)
		if err != nil {
			return nil, err
		}
		if identity == digest {
			reviewed[file.Path] = true
		}
	}
	return reviewed, nil
}

func (s *gitSession) SetFileReviewed(ctx context.Context, file File, reviewed bool) error {
	s.reviewMu.Lock()
	defer s.reviewMu.Unlock()
	name := s.reviewPath()
	state, err := readReviewFile(name)
	if err != nil {
		return err
	}
	if !reviewed {
		delete(state.Files, file.Path)
		return writeReviewFile(name, state)
	}
	identity, err := s.reviewFingerprint(ctx, file)
	if err != nil {
		return err
	}
	state.Files[file.Path] = identity
	return writeReviewFile(name, state)
}
