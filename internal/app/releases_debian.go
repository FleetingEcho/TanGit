package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

const (
	debianExecutable = "/opt/lazygitgo/lazygitgo"
	pkexecExecutable = "/usr/bin/pkexec"
	aptExecutable    = "/usr/bin/apt-get"
)

// The launcher in /usr/bin points to the executable installed by the .deb.
func runningDebianPackage() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	resolved, err := filepath.EvalSymlinks(exe)
	return err == nil && filepath.ToSlash(resolved) == debianExecutable
}

func usePackageUpdates(nativeEnabled bool) bool {
	return runtime.GOOS == "linux" && !mygo.IsDev() && (runningDebianPackage() || !nativeEnabled)
}

func debianInstallerAvailable() bool {
	for _, path := range []string{pkexecExecutable, aptExecutable} {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return false
		}
	}
	return runningDebianPackage()
}

func trustedDebianAsset(asset packageRelease) bool {
	u, err := url.Parse(asset.DebURL)
	return err == nil && u.Scheme == "https" && u.Host == "github.com" && u.User == nil &&
		u.Path == "/FleetingEcho/lazygitGo/releases/download/v"+asset.Version+"/"+asset.DebName &&
		asset.DebName == "lazygitgo_"+asset.Version+"_"+runtime.GOARCH+".deb" &&
		debianSHA256(asset.DebDigest) != ""
}

func debianSHA256(digest string) string {
	encoded, ok := strings.CutPrefix(digest, "sha256:")
	decoded, err := hex.DecodeString(encoded)
	if !ok || err != nil || len(decoded) != sha256.Size {
		return ""
	}
	return strings.ToLower(encoded)
}

func installDebianUpdate(ctx context.Context, release packageRelease, installing func()) error {
	if !trustedDebianAsset(release) {
		return errors.New("the Debian package has no verifiable release asset")
	}
	return applyDebianUpdate(ctx, &http.Client{Timeout: 5 * time.Minute}, release, func(path string) error {
		installing()
		cmd := exec.CommandContext(ctx, pkexecExecutable, "--disable-internal-agent",
			aptExecutable, "install", "-y", path)
		output, err := cmd.CombinedOutput()
		if err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) && exit.ExitCode() == 126 {
				return errors.New("system authorization was canceled")
			}
			return fmt.Errorf("APT could not install the update (%w): %s", err, strings.TrimSpace(string(output)))
		}
		return nil
	})
}

// applyDebianUpdate passes a private, verified file to the package manager.
// A partial download or a checksum mismatch never reaches the installer.
func applyDebianUpdate(ctx context.Context, client *http.Client, release packageRelease, install func(string) error) error {
	want := debianSHA256(release.DebDigest)
	if want == "" {
		return errors.New("the Debian package has no SHA-256 checksum")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, release.DebURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the package download returned %s", resp.Status)
	}

	dir, err := os.MkdirTemp("", "lazygitgo-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "lazygitgo.deb")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	sum := sha256.New()
	const maxSize = 256 << 20
	size, err := io.Copy(io.MultiWriter(file, sum), io.LimitReader(resp.Body, maxSize+1))
	closeErr := file.Close()
	if err := errors.Join(err, closeErr); err != nil {
		return err
	}
	if size > maxSize {
		return errors.New("the Debian package exceeds the download size limit")
	}
	if hex.EncodeToString(sum.Sum(nil)) != want {
		return errors.New("the Debian package does not match the release checksum")
	}
	return install(path)
}
