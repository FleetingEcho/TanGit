package app

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ulikunitz/xz"
)

func TestLanguageServerAssetRequiresAnExactHTTPSMatch(t *testing.T) {
	release := languageServerRelease{Assets: []languageServerReleaseAsset{
		{Name: "clangd-linux-23.1.0.zip.sig", URL: "https://example.test/signature"},
		{Name: "clangd-linux-23.1.0.zip", URL: "http://example.test/insecure"},
		{Name: "clangd-mac-23.1.0.zip", URL: "https://example.test/mac"},
	}}
	if _, err := languageServerAsset(release, "clangd-linux-23.1.0.zip"); err == nil {
		t.Fatal("an insecure or signature asset was accepted")
	}
	asset, err := languageServerAsset(release, "missing.zip", "clangd-mac-23.1.0.zip")
	if err != nil || asset.Name != "clangd-mac-23.1.0.zip" {
		t.Fatalf("exact fallback asset = %#v, %v", asset, err)
	}
}

func TestInstallLanguageServerAssetPublishesOnlyACompleteArchive(t *testing.T) {
	archive := languageServerZip(t, map[string]string{
		"package/bin/server":    "new server",
		"package/share/doc.txt": "documentation",
	}, 0o755)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_, _ = response.Write(archive)
	}))
	defer server.Close()
	root := filepath.Join(t.TempDir(), "tools", "lsp", "example")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "old"), []byte("previous"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := installLanguageServerAsset(context.Background(), root, languageServerReleaseAsset{
		Name: "server.zip", URL: server.URL,
	}, "package/bin/server")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "package", "bin", "server"))
	if err != nil || string(data) != "new server" {
		t.Fatalf("published executable = %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(root, "old")); !os.IsNotExist(err) {
		t.Fatalf("previous installation survived publication: %v", err)
	}
	if _, err := os.Stat(root + ".previous"); !os.IsNotExist(err) {
		t.Fatalf("installation backup was not removed: %v", err)
	}
}

func TestInstallLanguageServerAssetFailureKeepsPreviousInstallation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		_, _ = response.Write(languageServerZip(t, map[string]string{"other": "missing executable"}, 0o644))
	}))
	defer server.Close()
	root := filepath.Join(t.TempDir(), "example")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "current"), []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installLanguageServerAsset(context.Background(), root, languageServerReleaseAsset{
		Name: "server.zip", URL: server.URL,
	}, "bin/server"); err == nil {
		t.Fatal("an archive without its executable was published")
	}
	data, err := os.ReadFile(filepath.Join(root, "current"))
	if err != nil || string(data) != "keep me" {
		t.Fatalf("previous installation = %q, %v", data, err)
	}
}

func TestExtractLanguageServerArchivesAndRejectUnsafeEntries(t *testing.T) {
	for _, kind := range []string{"zip", "tar.gz", "tar.xz"} {
		t.Run(kind, func(t *testing.T) {
			directory := t.TempDir()
			var data []byte
			switch kind {
			case "zip":
				data = languageServerZip(t, map[string]string{"bin/server": "stdio"}, 0o755)
			case "tar.gz":
				data = languageServerTarGzip(t, map[string]string{"bin/server": "stdio"}, 0o755)
			case "tar.xz":
				data = languageServerTarXZ(t, map[string]string{"bin/server": "stdio"}, 0o755)
			}
			path := filepath.Join(directory, "server."+kind)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(directory, "output")
			if err := os.Mkdir(target, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := extractLanguageServerArchive(path, "server."+kind, target); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(target, "bin", "server"))
			if err != nil || string(data) != "stdio" {
				t.Fatalf("extracted executable = %q, %v", data, err)
			}
		})
	}

	for _, entry := range []string{"../escape", "/absolute", "bin/../../escape"} {
		t.Run("zip "+entry, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "bad.zip")
			data := languageServerZip(t, map[string]string{entry: "unsafe"}, 0o644)
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(directory, "output")
			if err := os.Mkdir(target, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := extractLanguageServerArchive(path, "bad.zip", target); err == nil {
				t.Fatalf("unsafe archive entry %q was accepted", entry)
			}
			if _, err := os.Stat(filepath.Join(directory, "escape")); !os.IsNotExist(err) {
				t.Fatal("archive entry escaped its target")
			}
		})
	}
}

func TestLanguageServerArchiveRejectsLinksAndWrongExecutable(t *testing.T) {
	directory := t.TempDir()
	var buffer bytes.Buffer
	archive := tar.NewWriter(&buffer)
	if err := archive.WriteHeader(&tar.Header{Name: "bin/server", Typeflag: tar.TypeSymlink, Linkname: "../../outside"}); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "server.tar.gz")
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	_, _ = gzipWriter.Write(buffer.Bytes())
	_ = gzipWriter.Close()
	if err := os.WriteFile(path, compressed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := extractLanguageServerArchive(path, "server.tar.gz", directory); err == nil {
		t.Fatal("archive symlink was accepted")
	}
	if _, err := languageServerReleaseExecutable(directory, "../outside"); err == nil {
		t.Fatal("executable outside its managed root was accepted")
	}
}

func languageServerZip(t *testing.T, files map[string]string, mode os.FileMode) []byte {
	t.Helper()
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for name, content := range files {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetMode(mode)
		entry, err := archive.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func languageServerTarGzip(t *testing.T, files map[string]string, mode int64) []byte {
	t.Helper()
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	writeLanguageServerTar(t, gzipWriter, files, mode)
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func languageServerTarXZ(t *testing.T, files map[string]string, mode int64) []byte {
	t.Helper()
	var compressed bytes.Buffer
	xzWriter, err := xz.NewWriter(&compressed)
	if err != nil {
		t.Fatal(err)
	}
	writeLanguageServerTar(t, xzWriter, files, mode)
	if err := xzWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func writeLanguageServerTar(t *testing.T, destination io.Writer, files map[string]string, mode int64) {
	t.Helper()
	archive := tar.NewWriter(destination)
	for name, content := range files {
		if err := archive.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(archive, strings.NewReader(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
}
