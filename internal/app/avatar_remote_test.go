package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
	"github.com/egoist/mygo/ui"
)

type avatarRoundTripper func(*http.Request) (*http.Response, error)

func (f avatarRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func avatarPNG(t *testing.T) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 120, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 120; x++ {
			picture.SetRGBA(x, y, color.RGBA{R: uint8(40 + x), G: uint8(100 + y), B: 180, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, picture); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func waitAvatar(t *testing.T, s *authorAvatarStore, source avatarSource) *ui.Bitmap {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		entry := s.entries[source.key()]
		done := entry != nil && !entry.pending
		var bitmap *ui.Bitmap
		if done {
			bitmap = entry.bitmap
		}
		s.mu.Unlock()
		if done {
			return bitmap
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("avatar worker did not complete")
	return nil
}

func TestAvatarSourcesUseTheConfiguredHost(t *testing.T) {
	commit := repository.Commit{Hash: strings.Repeat("a", 40), AuthorEmail: "Ada+Work@Example.COM"}
	for _, tc := range []struct{ remote, origin, tool string }{
		{"git@github.com:team/repo.git", "https://github.com", "gh"},
		{"https://gitlab.com/team/subgroup/repo.git", "https://gitlab.com", "glab"},
		{"git@code.company.internal:team/subgroup/repo.git", "https://code.company.internal", "glab"},
		{"ssh://git@code.company.internal:2222/team/repo.git", "https://code.company.internal", "glab"},
		{"http://code.company.internal:8080/team/repo.git", "http://code.company.internal:8080", "glab"},
		{"https://github.com.example.org/team/repo.git", "https://github.com.example.org", "glab"},
	} {
		t.Run(tc.remote, func(t *testing.T) {
			source, ok := avatarSourceFor(deriveRemoteWebURL(tc.remote), commit)
			if !ok || source.origin != tc.origin {
				t.Fatalf("source = %#v %v", source, ok)
			}
			address, endpoint, tool, _ := source.endpoint()
			if tool != tc.tool {
				t.Fatalf("tool = %q", tool)
			}
			if tool == "gh" {
				if !strings.HasPrefix(address, "https://api.github.com/repos/team/repo/commits?") {
					t.Fatal(address)
				}
			} else {
				if !strings.HasPrefix(address, tc.origin+"/api/v4/avatar?") {
					t.Fatal(address)
				}
				query, _ := url.ParseQuery(strings.SplitN(endpoint, "?", 2)[1])
				if query.Get("email") != "ada+work@example.com" {
					t.Fatal(query)
				}
			}
		})
	}
	for _, address := range []string{"/work/repo", "file:///tmp/repo", "https://user:password@github.com/team/repo"} {
		if _, ok := avatarSourceFor(address, commit); ok {
			t.Fatalf("accepted %q", address)
		}
	}
	first, _ := avatarSourceFor("https://code.company.internal/team/repo", commit)
	second, _ := avatarSourceFor("https://gitlab.com/team/repo", commit)
	if first.key() == second.key() {
		t.Fatal("different GitLab instances share an identity cache")
	}
}

func TestRemoteAvatarFetchesGitHubGitLabAndInternalGitLab(t *testing.T) {
	picture := avatarPNG(t)
	hash := strings.Repeat("a", 40)
	for _, provider := range []string{"github", "gitlab", "internal-gitlab", "authenticated-gitlab"} {
		t.Run(provider, func(t *testing.T) {
			var base string
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path == "/photo.png" {
					w.Header().Set("Content-Type", "image/png")
					_, _ = w.Write(picture)
					return
				}
				if provider == "github" {
					if r.URL.Path != "/repos/team/repo/commits" || r.URL.Query().Get("author") != "ada+work@example.com" || r.URL.Query().Get("per_page") != "1" {
						t.Errorf("GitHub lookup = %s", r.URL)
					}
					_ = json.NewEncoder(w).Encode([]map[string]any{{"sha": hash, "commit": map[string]any{"author": map[string]string{"email": "ada+work@example.com"}}, "author": map[string]string{"avatar_url": base + "/photo.png"}}})
					return
				}
				if r.URL.Path != "/api/v4/avatar" || r.URL.Query().Get("email") != "ada+work@example.com" {
					t.Errorf("GitLab lookup = %s", r.URL)
				}
				if provider == "authenticated-gitlab" {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"avatar_url": "/photo.png"})
			}))
			defer server.Close()
			base = server.URL
			store := newAuthorAvatarStore("")
			defer store.cancel()
			store.client = server.Client()
			transport := store.client.Transport
			store.client.Transport = avatarRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == "api.github.com" {
					clone := r.Clone(r.Context())
					u, _ := url.Parse(base + r.URL.RequestURI())
					clone.URL = u
					return transport.RoundTrip(clone)
				}
				return transport.RoundTrip(r)
			})
			origin := base
			if provider == "github" {
				origin = "https://github.com"
			}
			source, _ := avatarSourceFor(origin+"/team/repo", repository.Commit{Hash: hash, AuthorEmail: "Ada+Work@Example.COM"})
			var cliCalls atomic.Int32
			store.api = func(_ context.Context, tool, host, endpoint string) ([]byte, error) {
				cliCalls.Add(1)
				expected, _ := url.Parse(base)
				if tool != "glab" || host != expected.Host || !strings.HasPrefix(endpoint, "avatar?") {
					t.Errorf("CLI lookup = %q %q %q", tool, host, endpoint)
				}
				return []byte(`{"avatar_url":"/photo.png"}`), nil
			}
			bitmap, encoded := store.fetch(context.Background(), source)
			if bitmap == nil {
				t.Fatal("photo was not loaded")
			}
			width, height := bitmap.Size()
			if width != 96 || height != 96 {
				t.Fatalf("thumbnail = %dx%d", width, height)
			}
			if _, err := png.Decode(bytes.NewReader(encoded)); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 2 {
				t.Fatalf("HTTP requests = %d", calls.Load())
			}
			if provider == "authenticated-gitlab" && cliCalls.Load() != 1 {
				t.Fatal("existing GitLab authentication was not tried")
			}
			if provider != "authenticated-gitlab" && cliCalls.Load() != 0 {
				t.Fatal("public avatar invoked a hosting CLI")
			}
		})
	}
}

func TestRemoteAvatarCacheDeduplicatesAndWorksOffline(t *testing.T) {
	picture := avatarPNG(t)
	var base string
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/photo.png" {
			_, _ = w.Write(picture)
			return
		}
		close(started)
		<-release
		_ = json.NewEncoder(w).Encode(map[string]string{"avatar_url": base + "/photo.png"})
	}))
	defer server.Close()
	base = server.URL
	directory := t.TempDir()
	source, _ := avatarSourceFor(base+"/team/repo", repository.Commit{AuthorEmail: "ada@example.com"})
	store := newAuthorAvatarStore(directory)
	defer store.cancel()
	store.api = nil
	if store.lookup(source, nil) != nil {
		t.Fatal("first lookup must retain the initial")
	}
	<-started
	for range 20 {
		if store.lookup(source, nil) != nil {
			t.Fatal("unfinished lookup returned a photo")
		}
	}
	close(release)
	if waitAvatar(t, store, source) == nil || calls.Load() != 2 {
		t.Fatalf("deduplication failed: %d requests", calls.Load())
	}
	offline := func() *authorAvatarStore {
		cache := newAuthorAvatarStore(directory)
		t.Cleanup(cache.cancel)
		cache.api = nil
		cache.client = &http.Client{Transport: avatarRoundTripper(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") })}
		return cache
	}
	fresh := offline()
	fresh.lookup(source, nil)
	if waitAvatar(t, fresh, source) == nil {
		t.Fatal("fresh disk cache unavailable offline")
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("cache files = %v %v", entries, err)
	}
	path := filepath.Join(directory, entries[0].Name())
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	stale := offline()
	stale.lookup(source, nil)
	if waitAvatar(t, stale, source) == nil {
		t.Fatal("failed refresh discarded a stale cached photo")
	}
}

func TestRemoteAvatarFailuresKeepTheInitialAndBackOff(t *testing.T) {
	for _, failure := range []string{"missing", "restricted", "bad-json", "bad-image", "oversized", "timeout", "unknown-user"} {
		t.Run(failure, func(t *testing.T) {
			var base string
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				switch failure {
				case "missing":
					w.WriteHeader(404)
				case "restricted":
					w.WriteHeader(403)
				case "bad-json":
					_, _ = io.WriteString(w, "not-json")
				case "timeout":
					<-r.Context().Done()
				case "unknown-user":
					_, _ = io.WriteString(w, `{"avatar_url":""}`)
				default:
					if r.URL.Path != "/photo.png" {
						_ = json.NewEncoder(w).Encode(map[string]string{"avatar_url": base + "/photo.png"})
						return
					}
					if failure == "oversized" {
						_, _ = w.Write(bytes.Repeat([]byte{'x'}, avatarImageLimit+1))
					} else {
						_, _ = io.WriteString(w, "not an image")
					}
				}
			}))
			defer server.Close()
			base = server.URL
			store := newAuthorAvatarStore("")
			defer store.cancel()
			store.api = nil
			store.client.Timeout = 40 * time.Millisecond
			source, _ := avatarSourceFor(base+"/team/repo", repository.Commit{AuthorEmail: "ada@example.com"})
			store.lookup(source, nil)
			if waitAvatar(t, store, source) != nil {
				t.Fatal("failure returned a photo")
			}
			before := calls.Load()
			for range 10 {
				if store.lookup(source, nil) != nil {
					t.Fatal("failure stopped using the initial")
				}
			}
			if calls.Load() != before {
				t.Fatal("every frame retried a failed avatar")
			}
		})
	}
}

func TestRemoteAvatarNormalizesPhotoURLs(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{"/uploads/avatar.png", "https://code.internal/uploads/avatar.png"},
		{"javascript:alert(1)", ""}, {"file:///etc/passwd", ""}, {"https://user:secret@code.internal/photo.png", ""}, {"http://code.internal/photo.png", ""},
		{"https://code.internal/assets/no_avatar-123.png", ""},
	} {
		if got := normalizeAvatarURL("https://code.internal", tc.value); got != tc.want {
			t.Errorf("%q = %q, want %q", tc.value, got, tc.want)
		}
	}
	address := normalizeAvatarURL("https://code.internal", "https://secure.gravatar.com/avatar/abc?d=identicon&s=32")
	parsed, _ := url.Parse(address)
	if parsed.Query().Get("d") != "404" || parsed.Query().Get("s") != "96" {
		t.Fatal("missing accounts would replace initials with generated avatar images")
	}
}

func TestRemoteAvatarCancellationStopsOutstandingRequests(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(stopped) }))
	defer server.Close()
	store := newAuthorAvatarStore("")
	store.api = nil
	var redraws atomic.Int32
	source, _ := avatarSourceFor(server.URL+"/team/repo", repository.Commit{AuthorEmail: "ada@example.com"})
	store.lookup(source, func() { redraws.Add(1) })
	<-started
	store.cancel()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("window shutdown did not cancel the avatar request")
	}
	if store.lookup(source, nil) != nil || redraws.Load() != 0 {
		t.Fatal("closed avatar store published a photo or redraw")
	}
}

func TestAuthorAvatarLoadsWithoutChangingLayoutOrShowingErrors(t *testing.T) {
	picture := avatarPNG(t)
	var base string
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/photo.png" {
			_, _ = w.Write(picture)
			return
		}
		<-release
		_ = json.NewEncoder(w).Encode(map[string]string{"avatar_url": base + "/photo.png"})
	}))
	defer server.Close()
	base = server.URL
	store := newAuthorAvatarStore("")
	defer store.cancel()
	store.api = nil
	a := newApp(fakeOpener{})
	a.authorAvatars = store
	a.snapshot.Remotes = []repository.Remote{{Name: "origin", URLs: []string{base + "/team/repo.git"}}}
	commit := repository.Commit{AuthorName: "Ada Lovelace", AuthorEmail: "ada@example.com"}
	tester := ui.NewTester(func(c *ui.Context) {
		ui.Column(c).Fill().Padding(20).Background(c.Theme().Background).Children(func() {
			ui.Row(c).Gap(12).Children(func() {
				for _, size := range []float32{18, 24, 36} {
					a.authorAvatar(c, commit, size)
				}
			})
		})
	}, 400, 180)
	before, ok := tester.Find("Ada Lovelace")
	if !ok || !containsText(tester.Texts(), "A") {
		t.Fatal("pending photo did not show an initial")
	}
	close(release)
	source, _ := avatarSourceFor(base+"/team/repo", commit)
	if waitAvatar(t, store, source) == nil {
		t.Fatal("photo not loaded")
	}
	tester.Frame()
	after, ok := tester.Find("Ada Lovelace")
	if !ok || before != after || containsText(tester.Texts(), "A") {
		t.Fatalf("photo altered layout or retained initial: before=%+v after=%+v", before, after)
	}
	if dir := os.Getenv("MYGO_SHOTS"); dir != "" {
		saveScreenshot(t, dir, "remote-author-avatars-light", tester)
		tester.SetDark(true)
		saveScreenshot(t, dir, "remote-author-avatars-dark", tester)
	}
}

func TestAvatarLimitedOutputAndImageValidation(t *testing.T) {
	output := &avatarLimitedBuffer{limit: 4}
	if _, err := io.Copy(output, strings.NewReader("12345")); err == nil {
		t.Fatal("CLI output limit was bypassed")
	}
	for _, data := range [][]byte{nil, []byte("<svg/>"), []byte("not an image")} {
		if bitmap, _, err := decodeAuthorAvatar(data); bitmap != nil || err == nil {
			t.Fatal("invalid image accepted")
		}
	}
}

// Keep the default response JSON shape checked without a live hosting account.
func Example_avatarSource() {
	source, _ := avatarSourceFor("https://code.company.internal/team/repo", repository.Commit{AuthorEmail: "ada@example.com"})
	address, _, _, _ := source.endpoint()
	fmt.Println(address)
	// Output: https://code.company.internal/api/v4/avatar?email=ada%40example.com&size=96
}

func TestAuthorAvatarFetchFailuresNeverCreateNotifications(t *testing.T) {
	store := newAuthorAvatarStore("")
	defer store.cancel()
	store.client = &http.Client{Transport: avatarRoundTripper(func(*http.Request) (*http.Response, error) { return nil, errors.New("server unavailable") })}
	store.api = func(context.Context, string, string, string) ([]byte, error) {
		return nil, errors.New("please log in: credential detail")
	}
	a := newApp(fakeOpener{})
	a.authorAvatars = store
	a.snapshot.Remotes = []repository.Remote{{Name: "origin", URLs: []string{"https://code.internal/team/repo.git"}}}
	commit := repository.Commit{AuthorName: "Ada", AuthorEmail: "ada@example.com"}
	tester := ui.NewTester(func(c *ui.Context) { a.authorAvatar(c, commit, 36) }, 100, 100)
	source, _ := avatarSourceFor("https://code.internal/team/repo", commit)
	if waitAvatar(t, store, source) != nil {
		t.Fatal("failed avatar returned a bitmap")
	}
	tester.Frame()
	if !containsText(tester.Texts(), "A") || len(a.pendingToasts) != 0 || tester.HasText("unavailable") || tester.HasText("log in") {
		t.Fatalf("avatar failure reached the UI: %v", tester.Texts())
	}
}

func TestGitHubAvatarRequiresTheMatchingAuthor(t *testing.T) {
	hash := strings.Repeat("a", 40)
	for _, tc := range []struct{ name, email, body string }{
		{"unlinked author", "ada@example.com", `[{"sha":"` + hash + `","author":null}]`},
		{"different email", "ada@example.com", `[{"sha":"` + hash + `","commit":{"author":{"email":"other@example.com"}},"author":{"avatar_url":"https://example.org/wrong.png"}}]`},
		{"different hash", "", `[{"sha":"` + strings.Repeat("b", 40) + `","author":{"avatar_url":"https://example.org/wrong.png"}}]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newAuthorAvatarStore("")
			defer store.cancel()
			store.api = nil
			var imageRequests atomic.Int32
			store.client = &http.Client{Transport: avatarRoundTripper(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host != "api.github.com" {
					imageRequests.Add(1)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})}
			source, _ := avatarSourceFor("https://github.com/team/repo", repository.Commit{Hash: hash, AuthorEmail: tc.email})
			if bitmap, _ := store.fetch(context.Background(), source); bitmap != nil || imageRequests.Load() != 0 {
				t.Fatal("mismatched author displayed a photo")
			}
		})
	}
	// Email-based lookup also works when the currently displayed commit is local.
	source, ok := avatarSourceFor("https://github.com/team/repo", repository.Commit{AuthorEmail: "ada@example.com"})
	if !ok {
		t.Fatal("an unpublished commit blocked its author's avatar lookup")
	}
	_, endpoint, _, _ := source.endpoint()
	parsed, _ := url.Parse(endpoint)
	if parsed.Query().Get("author") != "ada@example.com" {
		t.Fatal(endpoint)
	}
}

// Run a worker synchronously here so a repaint assertion happens after its
// completion callback, rather than racing the worker's final notification.
func loadAvatarForRepaintTest(store *authorAvatarStore, source avatarSource, invalidate func()) *avatarEntry {
	store.mu.Lock()
	entry := store.entries[source.key()]
	if entry == nil {
		entry = &avatarEntry{}
		store.entries[source.key()] = entry
	}
	entry.pending = true
	entry.seenFrame = store.frame
	store.mu.Unlock()
	store.load(source, source.key(), entry, invalidate)
	return entry
}

func repaintAvatarStore(t *testing.T) (*authorAvatarStore, avatarSource, *atomic.Int32) {
	t.Helper()
	picture := avatarPNG(t)
	var base string
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/photo.png" {
			_, _ = w.Write(picture)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"avatar_url": base + "/photo.png"})
	}))
	base = server.URL
	store := newAuthorAvatarStore(t.TempDir())
	store.api = nil
	t.Cleanup(func() { store.cancel(); server.Close() })
	source, _ := avatarSourceFor(base+"/team/repo", repository.Commit{AuthorEmail: "ada@example.com"})
	return store, source, &requests
}

func TestAvatarRefreshDoesNotRepaintUnchangedPhotos(t *testing.T) {
	store, source, _ := repaintAvatarStore(t)
	redraws := 0
	store.beginFrame(true)
	entry := loadAvatarForRepaintTest(store, source, func() { redraws++ })
	if entry.bitmap == nil || redraws != 1 {
		t.Fatal("first visible photo did not request one repaint")
	}
	original := entry.bitmap
	// Expire the disk cache as well as the in-memory entry, forcing a network
	// refresh. Neither decoding the same disk photo nor downloading the same
	// pixels should replace the bitmap or repaint the window.
	files, err := os.ReadDir(store.directory)
	if err != nil || len(files) != 1 {
		t.Fatalf("cache files: %v %v", files, err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(store.directory, files[0].Name()), old, old); err != nil {
		t.Fatal(err)
	}
	store.beginFrame(true)
	loadAvatarForRepaintTest(store, source, func() { redraws++ })
	if redraws != 1 || entry.bitmap != original {
		t.Fatalf("unchanged refresh replaced the bitmap or repainted: redraws=%d", redraws)
	}
}

func TestAvatarCompletionsCoalesceUntilTheNextFrame(t *testing.T) {
	store, source, _ := repaintAvatarStore(t)
	redraws := 0
	store.beginFrame(true)
	for i := 0; i < 8; i++ {
		author := source
		author.email = fmt.Sprintf("author-%d@example.com", i)
		entry := loadAvatarForRepaintTest(store, author, func() { redraws++ })
		if entry.bitmap == nil {
			t.Fatal("coalesced update discarded an author's photo")
		}
	}
	if redraws != 1 {
		t.Fatalf("eight completions before a frame requested %d repaints, want 1", redraws)
	}
	store.beginFrame(true)
	source.email = "next-frame@example.com"
	loadAvatarForRepaintTest(store, source, func() { redraws++ })
	if redraws != 2 {
		t.Fatalf("next frame could not request a new repaint: %d", redraws)
	}
}

func TestAvatarHiddenResultsPopulateCacheWithoutRepainting(t *testing.T) {
	for _, reason := range []string{"page changed", "window hidden", "window closed"} {
		t.Run(reason, func(t *testing.T) {
			store, source, _ := repaintAvatarStore(t)
			store.beginFrame(true)
			entry := &avatarEntry{pending: true, seenFrame: store.frame}
			store.entries[source.key()] = entry
			switch reason {
			case "page changed":
				store.beginFrame(true) // the new page does not look up this author
			case "window hidden":
				shell := newShell(fakeOpener{})
				shell.authorAvatars = store
				shell.handleWindowHidden()
			case "window closed":
				store.cancel()
			}
			redraws := 0
			store.load(source, source.key(), entry, func() { redraws++ })
			if redraws != 0 {
				t.Fatalf("%s result requested a repaint", reason)
			}
			if reason != "window closed" && entry.bitmap == nil {
				t.Fatal("hidden result was not cached for the next visit")
			}
		})
	}
}

func TestCachedAvatarsLeaveTheUIIdle(t *testing.T) {
	store, source, requests := repaintAvatarStore(t)
	store.beginFrame(true)
	entry := loadAvatarForRepaintTest(store, source, nil)
	if entry.bitmap == nil {
		t.Fatal("failed to prepare a cached photo")
	}
	a := newApp(fakeOpener{})
	a.authorAvatars = store
	a.snapshot.Remotes = []repository.Remote{{Name: "origin", URLs: []string{source.origin + "/team/repo.git"}}}
	builds := 0
	tester := ui.NewTester(func(c *ui.Context) {
		builds++
		store.beginFrame(true)
		a.authorAvatar(c, repository.Commit{AuthorName: "Ada", AuthorEmail: source.email}, 36)
	}, 100, 100)
	before := requests.Load()
	for range 20 {
		tester.Frame()
		if tester.FramePending() {
			t.Fatal("rendering a cached avatar scheduled another frame")
		}
	}
	if requests.Load() != before || builds > 24 {
		t.Fatalf("cached avatars kept working: HTTP requests=%d builds=%d", requests.Load(), builds)
	}
}
