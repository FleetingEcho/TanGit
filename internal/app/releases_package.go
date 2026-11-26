package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Package-managed builds read the release feed directly so the Debian
// installation can be updated through APT.

// latestReleaseAPI is where the newest published release is read from. It is a
// variable so tests can point it at a local server.
var latestReleaseAPI = "https://api.github.com/repos/FleetingEcho/lazygitGo/releases/latest"

// releasesWebBase serves the release pages and assets. Unlike the API it has
// no per-address request limit, so it answers when the API refuses.
var releasesWebBase = "https://github.com/FleetingEcho/lazygitGo/releases"

// packageRelease is a published release the running build is behind.
type packageRelease struct {
	Version string
	Page    string
	// DebURL is the .deb for this machine's architecture, "" when none is listed.
	DebURL    string
	DebName   string
	DebDigest string
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Draft   bool   `json:"draft"`
	Assets  []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

// fetchLatestRelease reads the newest release, with its .deb for arch.
func fetchLatestRelease(ctx context.Context, client *http.Client, arch string) (packageRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, latestReleaseAPI, nil)
	if err != nil {
		return packageRelease{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "lazygitGo-update-check")
	response, err := client.Do(request)
	if err != nil {
		return packageRelease{}, err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
		// An address that has used up the API's anonymous allowance gets 403.
		// The release page and its checksum file are not limited that way.
		return fetchLatestReleaseFromWeb(ctx, client, arch)
	}
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
		if asset.Name == "lazygitgo_"+result.Version+"_"+arch+".deb" {
			result.DebURL, result.DebName, result.DebDigest = asset.URL, asset.Name, asset.Digest
			break
		}
	}
	return result, nil
}

// fetchLatestReleaseFromWeb finds the newest release without the API: the
// latest-release page redirects to the tag, and the release's SHA256SUMS lists
// the checksum of each package it ships.
func fetchLatestReleaseFromWeb(ctx context.Context, client *http.Client, arch string) (packageRelease, error) {
	noRedirect := *client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesWebBase+"/latest", nil)
	if err != nil {
		return packageRelease{}, err
	}
	request.Header.Set("User-Agent", "lazygitGo-update-check")
	response, err := noRedirect.Do(request)
	if err != nil {
		return packageRelease{}, err
	}
	response.Body.Close()
	location, err := response.Location()
	if err != nil {
		return packageRelease{}, fmt.Errorf("the release page answered %s", response.Status)
	}
	tag := path.Base(location.Path)
	version := strings.TrimPrefix(tag, "v")
	if !strings.HasPrefix(tag, "v") || version == "" || strings.ContainsAny(version, "/?#") {
		return packageRelease{}, fmt.Errorf("the release page named no version")
	}
	result := packageRelease{Version: version, Page: releasesWebBase + "/tag/" + tag}

	name := "lazygitgo_" + version + "_" + arch + ".deb"
	request, err = http.NewRequestWithContext(ctx, http.MethodGet, releasesWebBase+"/download/"+tag+"/SHA256SUMS", nil)
	if err != nil {
		return packageRelease{}, err
	}
	request.Header.Set("User-Agent", "lazygitGo-update-check")
	sums, err := client.Do(request)
	if err != nil {
		return packageRelease{}, err
	}
	defer sums.Body.Close()
	if sums.StatusCode != http.StatusOK {
		// A release without checksums can still be announced; it cannot be
		// installed, because the installer needs a digest to verify.
		return result, nil
	}
	body, err := io.ReadAll(io.LimitReader(sums.Body, 1<<20))
	if err != nil {
		return packageRelease{}, err
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == name {
			result.DebURL = releasesWebBase + "/download/" + tag + "/" + name
			result.DebName, result.DebDigest = name, "sha256:"+fields[0]
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
		return trf("lazygitGo %s is available. Download it from the releases page.", release.Version)
	}
	return trf("lazygitGo %s is available. Download %s, then install it with: sudo apt install ./%s",
		release.Version, release.DebName, release.DebName)
}
