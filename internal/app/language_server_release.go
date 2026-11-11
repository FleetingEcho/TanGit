package app

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ulikunitz/xz"
)

const (
	maxLanguageServerReleaseDownload = 256 << 20
	maxLanguageServerReleaseExtract  = 512 << 20
)

type languageServerReleaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type languageServerRelease struct {
	Tag    string                       `json:"tag_name"`
	Assets []languageServerReleaseAsset `json:"assets"`
}

var languageServerReleaseHTTPClient = http.DefaultClient

func latestLanguageServerRelease(ctx context.Context, repository string) (languageServerRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/"+repository+"/releases/latest", nil)
	if err != nil {
		return languageServerRelease{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "TanGit language-server installer")
	response, err := languageServerReleaseHTTPClient.Do(request)
	if err != nil {
		return languageServerRelease{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return languageServerRelease{}, fmt.Errorf("find language-server release: %s", response.Status)
	}
	var release languageServerRelease
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&release); err != nil {
		return languageServerRelease{}, fmt.Errorf("read language-server release: %w", err)
	}
	return release, nil
}

func languageServerAsset(release languageServerRelease, names ...string) (languageServerReleaseAsset, error) {
	for _, name := range names {
		for _, asset := range release.Assets {
			if asset.Name == name && strings.HasPrefix(asset.URL, "https://") {
				return asset, nil
			}
		}
	}
	if len(names) == 0 {
		return languageServerReleaseAsset{}, errors.New("no language-server download was specified for this platform")
	}
	return languageServerReleaseAsset{}, fmt.Errorf("the latest language-server release has no %s download", strings.Join(names, " or "))
}

// installLanguageServerAsset unpacks a release into a new sibling directory,
// verifies the requested executable, and only then swaps it into place. A
// failed update leaves the previous managed installation untouched.
func installLanguageServerAsset(ctx context.Context, root string, asset languageServerReleaseAsset, executable string) error {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(executable) == "" {
		return errors.New("language-server installation path is empty")
	}
	parent := filepath.Dir(root)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".lsp-install-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	archivePath := filepath.Join(staging, "download"+languageServerArchiveExtension(asset.Name))
	if err := downloadLanguageServerRelease(ctx, asset.URL, archivePath); err != nil {
		return err
	}
	payload := filepath.Join(staging, "payload")
	if err := os.Mkdir(payload, 0o755); err != nil {
		return err
	}
	if err := extractLanguageServerArchive(archivePath, asset.Name, payload); err != nil {
		return err
	}
	program, err := languageServerReleaseExecutable(payload, executable)
	if err != nil {
		return err
	}
	if err := os.Chmod(program, 0o755); err != nil {
		return err
	}
	backup := root + ".previous"
	if err := os.RemoveAll(backup); err != nil {
		return err
	}
	if _, err := os.Lstat(root); err == nil {
		if err := os.Rename(root, backup); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(payload, root); err != nil {
		if _, backupErr := os.Lstat(backup); backupErr == nil {
			_ = os.Rename(backup, root)
		}
		return err
	}
	// The new installation is already valid. A stale backup must not turn a
	// successful update into a configuration rollback; retry cleanup next time.
	_ = os.RemoveAll(backup)
	return nil
}

func languageServerArchiveExtension(name string) string {
	lower := strings.ToLower(name)
	for _, extension := range []string{".tar.gz", ".tgz", ".tar.xz", ".zip"} {
		if strings.HasSuffix(lower, extension) {
			return extension
		}
	}
	return ""
}

func languageServerReleaseExecutable(root, relative string) (string, error) {
	if filepath.IsAbs(relative) || filepath.Base(relative) != relative && !filepath.IsLocal(relative) {
		return "", errors.New("language-server executable path is invalid")
	}
	path := filepath.Join(root, filepath.FromSlash(relative))
	within, err := filepath.Rel(root, path)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", errors.New("language-server executable escapes its managed directory")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("find installed language-server executable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("installed language-server executable is not a regular file")
	}
	return path, nil
}

func languageServerFindReleaseExecutable(root, base string) (string, error) {
	var matches []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() != base {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("language-server executable %q is not a regular file", entry.Name())
		}
		matches = append(matches, path)
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(matches) != 1 {
		return "", fmt.Errorf("find installed language-server executable %q: found %d candidates", base, len(matches))
	}
	return matches[0], nil
}

func downloadLanguageServerRelease(ctx context.Context, source, target string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "TanGit language-server installer")
	response, err := languageServerReleaseHTTPClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download language server: %s", response.Status)
	}
	if response.ContentLength > maxLanguageServerReleaseDownload {
		return errors.New("language-server download is larger than 256 MB")
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, io.LimitReader(response.Body, maxLanguageServerReleaseDownload+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return errors.Join(copyErr, closeErr)
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	if info.Size() > maxLanguageServerReleaseDownload {
		return errors.New("language-server download is larger than 256 MB")
	}
	return nil
}

func extractLanguageServerArchive(path, name, target string) error {
	switch languageServerArchiveExtension(name) {
	case ".zip":
		return extractLanguageServerZip(path, target)
	case ".tar.gz", ".tgz":
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		compressed, err := gzip.NewReader(file)
		if err != nil {
			return err
		}
		defer compressed.Close()
		return extractLanguageServerTar(tar.NewReader(compressed), target)
	case ".tar.xz":
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		compressed, err := xz.NewReader(file)
		if err != nil {
			return err
		}
		return extractLanguageServerTar(tar.NewReader(compressed), target)
	case "":
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(target, filepath.Base(name)), data, 0o755)
	default:
		return errors.New("unsupported language-server archive")
	}
}

func extractLanguageServerZip(path, target string) error {
	archive, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer archive.Close()
	var total int64
	for _, entry := range archive.File {
		name, err := languageServerArchivePath(target, entry.Name)
		if err != nil {
			return err
		}
		if entry.FileInfo().IsDir() {
			if err := os.MkdirAll(name, 0o755); err != nil {
				return err
			}
			continue
		}
		if !entry.Mode().IsRegular() {
			return fmt.Errorf("language-server archive contains unsupported file %q", entry.Name)
		}
		if entry.UncompressedSize64 > uint64(maxLanguageServerReleaseExtract-total) {
			return errors.New("language-server archive expands beyond 512 MB")
		}
		total += int64(entry.UncompressedSize64)
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			return err
		}
		source, err := entry.Open()
		if err != nil {
			return err
		}
		mode := entry.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		err = writeLanguageServerArchiveFile(name, source, mode)
		closeErr := source.Close()
		if err != nil || closeErr != nil {
			return errors.Join(err, closeErr)
		}
	}
	return nil
}

func extractLanguageServerTar(archive *tar.Reader, target string) error {
	var total int64
	for {
		entry, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		name, err := languageServerArchivePath(target, entry.Name)
		if err != nil {
			return err
		}
		switch entry.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(name, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			total += entry.Size
			if total > maxLanguageServerReleaseExtract {
				return errors.New("language-server archive expands beyond 512 MB")
			}
			if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(entry.Mode).Perm()
			if mode == 0 {
				mode = 0o644
			}
			if err := writeLanguageServerArchiveFile(name, io.LimitReader(archive, entry.Size), mode); err != nil {
				return err
			}
		case tar.TypeXGlobalHeader, tar.TypeXHeader, tar.TypeGNULongName, tar.TypeGNULongLink:
			// Metadata is consumed by archive/tar and has no filesystem entry.
		default:
			return fmt.Errorf("language-server archive contains unsupported file %q", entry.Name)
		}
	}
}

func languageServerArchivePath(root, name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(name, "/") || strings.TrimSpace(name) == "" || !filepath.IsLocal(filepath.FromSlash(name)) {
		return "", fmt.Errorf("language-server archive contains unsafe path %q", name)
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if clean == "." {
		return root, nil
	}
	path := filepath.Join(root, clean)
	within, err := filepath.Rel(root, path)
	if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("language-server archive contains unsafe path %q", name)
	}
	return path, nil
}

func writeLanguageServerArchiveFile(path string, source io.Reader, mode os.FileMode) (returnErr error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := file.Close(); returnErr == nil {
			returnErr = closeErr
		}
	}()
	_, err = io.Copy(file, source)
	return err
}
