package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// A copy installed by a package manager, such as the .deb in /opt, cannot
// replace itself, and a repository without an apt source never gets updates
// from the package manager either. For those installs TanGit checks GitHub's
// latest release itself and points to the package to install.

// latestReleaseAPI is where the newest published release is read from. It is a
// variable so tests can point it at a local server.
var latestReleaseAPI = "https://api.github.com/repos/FleetingEcho/TanGit/releases/latest"

// packageRelease is a published release the running build is behind.
type packageRelease struct {
	Version string
	Page    string
	// DebURL is the .deb for this machine's architecture, "" when none is listed.
	DebURL  string
	DebName string
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Draft   bool   `json:"draft"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// fetchLatestRelease reads the newest release, with its .deb for arch.
func fetchLatestRelease(ctx context.Context, client *http.Client, arch string) (packageRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseAPI, nil)
	if err != nil {
		return packageRelease{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "TanGit-update-check")
	response, err := client.Do(request)
	if err != nil {
		return packageRelease{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return packageRelease{}, fmt.Errorf("the release feed answered %s", response.Status)
	}
	var release githubRelease
	if err := json.NewDecoder(http.MaxBytesReader(nil, response.Body, 4<<20)).Decode(&release); err != nil {
		return packageRelease{}, err
	}
	if release.Draft || strings.TrimSpace(release.TagName) == "" {
		return packageRelease{}, fmt.Errorf("the release feed has no published release")
	}
	result := packageRelease{Version: strings.TrimPrefix(strings.TrimSpace(release.TagName), "v"), Page: release.HTMLURL}
	for _, asset := range release.Assets {
		if strings.HasSuffix(asset.Name, "_"+arch+".deb") {
			result.DebURL, result.DebName = asset.URL, asset.Name
			break
		}
	}
	return result, nil
}

// versionNewer reports whether latest is a higher dotted version than current.
func versionNewer(latest, current string) bool {
	parse := func(version string) []int {
		version = strings.TrimPrefix(strings.TrimSpace(version), "v")
		version, _, _ = strings.Cut(version, "-")
		var parts []int
		for _, field := range strings.Split(version, ".") {
			number, err := strconv.Atoi(field)
			if err != nil {
				number = 0
			}
			parts = append(parts, number)
		}
		return parts
	}
	a, b := parse(latest), parse(current)
	for index := 0; index < max(len(a), len(b)); index++ {
		var x, y int
		if index < len(a) {
			x = a[index]
		}
		if index < len(b) {
			y = b[index]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

// checkPackageRelease asks GitHub for a release newer than this build and
// reports it, or nil when the build is current.
func checkPackageRelease(ctx context.Context) (*packageRelease, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	release, err := fetchLatestRelease(ctx, &http.Client{Timeout: 20 * time.Second}, runtime.GOARCH)
	if err != nil {
		return nil, err
	}
	if !versionNewer(release.Version, currentAppVersion()) {
		return nil, nil
	}
	return &release, nil
}

// packageUpdateAdvice says how to install a release from its .deb.
func packageUpdateAdvice(release packageRelease) string {
	if release.DebName == "" {
		return trf("TanGit %s is available. Download it from the releases page.", release.Version)
	}
	return trf("TanGit %s is available. Download %s, then install it with: sudo apt install ./%s",
		release.Version, release.DebName, release.DebName)
}
