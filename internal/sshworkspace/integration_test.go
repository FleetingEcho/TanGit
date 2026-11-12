package sshworkspace

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/user"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestSSHIntegration is opt-in. Set TANGIT_SSH_TEST_ADDR to host:port and use
// the current user's SSH agent, or set TANGIT_SSH_TEST_AUTH=password/key and
// TANGIT_SSH_TEST_PASSWORD / TANGIT_SSH_TEST_KEY and optionally
// TANGIT_SSH_TEST_PASSPHRASE. The test uses a temporary known_hosts file and a
// unique directory beneath the remote home directory.
func TestSSHIntegration(t *testing.T) {
	address := os.Getenv("TANGIT_SSH_TEST_ADDR")
	if address == "" {
		t.Skip("set TANGIT_SSH_TEST_ADDR to run SSH integration tests")
	}
	profile := integrationProfile(t, address)
	store := NewMemorySecretStore()
	if profile.AuthMode == AuthPassword {
		if err := store.Set(PasswordSecretRef(profile.ID), os.Getenv("TANGIT_SSH_TEST_PASSWORD")); err != nil {
			t.Fatal(err)
		}
	}
	if profile.AuthMode == AuthKey && os.Getenv("TANGIT_SSH_TEST_PASSPHRASE") != "" {
		if err := store.Set(PassphraseSecretRef(profile.ID), os.Getenv("TANGIT_SSH_TEST_PASSPHRASE")); err != nil {
			t.Fatal(err)
		}
	}
	connector, err := NewConnector(store, filepath.Join(t.TempDir(), "known_hosts"), func(HostKeyInfo) (bool, error) {
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	client, err := connector.Connect(ctx, profile)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	result, err := client.Run(ctx, "", "printf", "%s", "hello 'quoted' $HOME")
	if err != nil {
		t.Fatalf("quoted command: %v", err)
	}
	if result.ExitCode != 0 || string(result.Output) != "hello 'quoted' $HOME" {
		t.Fatalf("quoted command result = %+v", result)
	}
	home, err := client.HomeDirectory(ctx)
	if err != nil {
		t.Fatal(err)
	}
	suffix, err := randomSuffix()
	if err != nil {
		t.Fatal(err)
	}
	remoteRoot := path.Join(home, ".tangit-sshworkspace-test-"+suffix)
	if err := client.MkdirAll(remoteRoot); err != nil {
		t.Fatal(err)
	}
	defer client.RemoveAll(remoteRoot)

	remoteFile := path.Join(remoteRoot, "atomic file.txt")
	if err := client.WriteFileAtomic(remoteFile, []byte("first"), 0o640); err != nil {
		t.Fatalf("first atomic write: %v", err)
	}
	if err := client.WriteFileAtomic(remoteFile, []byte("second"), 0o640); err != nil {
		t.Fatalf("replacement atomic write: %v", err)
	}
	if contents, err := client.ReadFile(remoteFile); err != nil || string(contents) != "second" {
		t.Fatalf("ReadFile()=%q, %v", contents, err)
	}
	workingDirResult, err := client.Run(ctx, remoteRoot, "pwd")
	if err != nil || strings.TrimSpace(string(workingDirResult.Output)) != remoteRoot {
		t.Fatalf("working-directory command = %+v, %v", workingDirResult, err)
	}

	localRoot := t.TempDir()
	localUpload := filepath.Join(localRoot, "upload.txt")
	if err := os.WriteFile(localUpload, []byte("uploaded contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	var uploadProgress TransferProgress
	if err := client.UploadFile(ctx, localUpload, path.Join(remoteRoot, "upload.txt"), 0, func(progress TransferProgress) {
		uploadProgress = progress
	}); err != nil {
		t.Fatal(err)
	}
	if uploadProgress.Items != 1 || uploadProgress.Bytes != int64(len("uploaded contents")) {
		t.Fatalf("upload progress = %+v", uploadProgress)
	}
	localDownload := filepath.Join(localRoot, "download.txt")
	if err := client.DownloadFile(ctx, path.Join(remoteRoot, "upload.txt"), localDownload, 0, nil); err != nil {
		t.Fatal(err)
	}
	if contents, err := os.ReadFile(localDownload); err != nil || string(contents) != "uploaded contents" {
		t.Fatalf("downloaded contents = %q, %v", contents, err)
	}

	uploadFolder := filepath.Join(localRoot, "folder")
	if err := os.MkdirAll(filepath.Join(uploadFolder, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uploadFolder, "nested", "file.txt"), []byte("nested"), 0o600); err != nil {
		t.Fatal(err)
	}
	remoteFolder := path.Join(remoteRoot, "folder")
	if err := client.UploadFolder(ctx, uploadFolder, remoteFolder, nil); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	zipWriter := zip.NewWriter(&archive)
	if err := client.DownloadFolderZIP(ctx, remoteFolder, zipWriter, nil); err != nil {
		_ = zipWriter.Close()
		t.Fatal(err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	zipReader, err := zip.NewReader(bytes.NewReader(archive.Bytes()), int64(archive.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(zipReader.File) != 1 || zipReader.File[0].Name != "nested/file.txt" {
		t.Fatalf("unexpected ZIP entries: %v", zipReader.File)
	}
	reader, err := zipReader.File[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	contents, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || string(contents) != "nested" {
		t.Fatalf("ZIP contents = %q, read=%v close=%v", contents, readErr, closeErr)
	}
}

func integrationProfile(t *testing.T, address string) Profile {
	t.Helper()
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		if strings.Contains(address, ":") {
			t.Fatalf("TANGIT_SSH_TEST_ADDR must be host:port (bracket IPv6): %v", err)
		}
		host, portText = address, strconv.Itoa(DefaultPort)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("invalid integration port: %v", err)
	}
	username := os.Getenv("TANGIT_SSH_TEST_USER")
	if username == "" {
		current, err := user.Current()
		if err != nil {
			t.Fatal(err)
		}
		username = current.Username
	}
	authMode := AuthMode(os.Getenv("TANGIT_SSH_TEST_AUTH"))
	if authMode == "" {
		authMode = AuthAgent
	}
	profile := Profile{
		ID: "integration", Name: "Integration", Host: host, Username: username,
		Port: port, AuthMode: authMode, PrivateKeyPath: os.Getenv("TANGIT_SSH_TEST_KEY"),
	}
	if err := profile.Validate(); err != nil {
		t.Fatal(fmt.Errorf("invalid integration profile: %w", err))
	}
	return profile
}
