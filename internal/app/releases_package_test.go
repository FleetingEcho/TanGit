package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
			{"name":"tangit_1.24.0_arm64.deb","browser_download_url":"https://example.test/arm.deb"},
			{"name":"tangit_1.24.0_amd64.deb","browser_download_url":"https://example.test/amd.deb"}]}`))
	}))
	defer server.Close()
	previous := latestReleaseAPI
	latestReleaseAPI = server.URL
	defer func() { latestReleaseAPI = previous }()
	release, err := fetchLatestRelease(context.Background(), server.Client(), "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if release.Version != "1.24.0" || release.DebURL != "https://example.test/amd.deb" || release.DebName != "tangit_1.24.0_amd64.deb" {
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
