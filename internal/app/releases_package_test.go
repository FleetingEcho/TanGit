package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

func TestPackageUpdateOpensInstallDialog(t *testing.T) {
	s, tt, updated := newShellTester(t, repository.Snapshot{Root: "/work/project"})
	name := "lazygitgo_1.25.0_" + runtime.GOARCH + ".deb"
	release := packageRelease{
		Version: "1.25.0", DebName: name,
		DebURL: projectRepositoryURL + "/releases/download/v1.25.0/" + name, DebDigest: "sha256:" + strings.Repeat("a", 64),
	}
	installed := make(chan packageRelease, 1)
	restarted := false
	s.packageUpdate.install = func(_ context.Context, got packageRelease, installing func()) error {
		installed <- got
		installing()
		return nil
	}
	s.packageUpdate.restart = func() { restarted = true }
	watchPackageUpdates(s, func(context.Context) (*packageRelease, error) { return &release, nil })
	updated.waitForUpdate(t)
	tt.Frame()
	if _, found := tt.Find("Software update"); found {
		t.Fatal("the background check opened a dialog")
	}
	if err := tt.Click("Update available"); err != nil {
		t.Fatal(err)
	}
	if _, found := tt.Find("Software update"); !found {
		t.Fatalf("no install dialog: %v", tt.Texts())
	}
	updated.waitForUpdate(t)
	tt.Frame()
	if err := tt.Click("Download and install"); err != nil {
		t.Fatal(err)
	}
	updated.waitForPending(t, 2)
	tt.Frame()
	if got := <-installed; got != release {
		t.Fatalf("installer received %+v, want %+v", got, release)
	}
	if err := tt.Click("Restart now"); err != nil {
		t.Fatal(err)
	}
	if !restarted || s.availableUpdate != "" {
		t.Fatalf("restart called=%v, advertised version=%q", restarted, s.availableUpdate)
	}
}

func TestDebianUpdateVerifiesTheDownloadBeforeInstalling(t *testing.T) {
	const payload = "package contents"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(server.Close)
	goodDigest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(payload)))
	for _, digest := range []string{"", "sha256:" + strings.Repeat("a", 64), goodDigest} {
		var installedPath string
		err := applyDebianUpdate(context.Background(), server.Client(), packageRelease{DebURL: server.URL, DebDigest: digest}, func(path string) error {
			installedPath = path
			data, err := os.ReadFile(path)
			if err == nil && string(data) != payload {
				t.Errorf("installed bytes = %q, want %q", data, payload)
			}
			return err
		})
		if digest == goodDigest {
			if err != nil || installedPath == "" {
				t.Fatalf("verified update did not reach installer: %v", err)
			}
			if _, err := os.Stat(installedPath); !os.IsNotExist(err) {
				t.Errorf("temporary package was not removed: %v", err)
			}
		} else if err == nil || installedPath != "" {
			t.Errorf("invalid digest reached installer: digest=%q path=%q err=%v", digest, installedPath, err)
		}
	}
}

func TestVersionNewerComparesDottedNumbers(t *testing.T) {
	for _, test := range []struct {
		latest, current string
		want            bool
	}{
		{"1.24.0", "1.23.0", true}, {"1.23.0", "1.23.0", false}, {"1.9.0", "1.23.0", false},
		{"v2.0", "1.99.9", true}, {"1.23.1", "1.23", true}, {"1.23.0-beta", "1.23.0", false},
	} {
		if got := versionNewer(test.latest, test.current); got != test.want {
			t.Errorf("versionNewer(%q, %q) = %v, want %v", test.latest, test.current, got, test.want)
		}
	}
}

func TestFetchLatestReleaseFindsTheDebForTheArchitecture(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.24.0","html_url":"https://example.test/r","assets":[
			{"name":"lazygitgo_1.24.0_arm64.deb","browser_download_url":"https://example.test/arm.deb"},
			{"name":"lazygitgo_1.24.0_amd64.deb","browser_download_url":"https://example.test/amd.deb","digest":"sha256:test"}]}`))
	}))
	defer server.Close()
	previous := latestReleaseAPI
	latestReleaseAPI = server.URL
	defer func() { latestReleaseAPI = previous }()
	release, err := fetchLatestRelease(context.Background(), server.Client(), "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if release.Version != "1.24.0" || release.DebURL != "https://example.test/amd.deb" || release.DebName != "lazygitgo_1.24.0_amd64.deb" || release.DebDigest != "sha256:test" {
		t.Fatalf("release = %+v", release)
	}
	if advice := packageUpdateAdvice(release); advice == "" {
		t.Fatal("no advice")
	}
}

func TestFetchLatestReleaseRejectsAFailingFeed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", 500) }))
	defer server.Close()
	previous := latestReleaseAPI
	latestReleaseAPI = server.URL
	defer func() { latestReleaseAPI = previous }()
	if _, err := fetchLatestRelease(context.Background(), server.Client(), "amd64"); err == nil {
		t.Fatal("a failing feed should be an error")
	}
}
