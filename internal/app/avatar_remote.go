package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"golang.org/x/image/draw"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

const (
	avatarImageLimit  = 1 << 20
	avatarMemoryLimit = 256
	avatarDiskLimit   = 512
	avatarFreshFor    = 24 * time.Hour
	avatarRetryAfter  = 5 * time.Minute
)

type avatarSource struct {
	origin, project, email, hash string
	github                       bool
	// direct is a picture address already known, as a merge request lists its
	// author's; no account lookup is needed then.
	direct string
}

// Custom remote hosts are tried as GitLab instances. An unsupported server
// simply leaves the initial in place; company domains need no name heuristic.
func avatarSourceFor(web string, commit repository.Commit) (avatarSource, bool) {
	normalized, ok := normalizeRemoteWebURL(web)
	if !ok {
		return avatarSource{}, false
	}
	u, _ := url.Parse(normalized)
	source := avatarSource{
		origin: u.Scheme + "://" + strings.ToLower(u.Host), project: strings.Trim(u.Path, "/"),
		email: strings.ToLower(strings.TrimSpace(commit.AuthorEmail)), hash: strings.ToLower(commit.Hash),
		github: strings.EqualFold(u.Hostname(), "github.com"),
	}
	if source.github {
		parts := strings.Split(source.project, "/")
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" || (source.email == "" && !avatarCommitHash(source.hash)) {
			return avatarSource{}, false
		}
	} else if source.email == "" || !strings.Contains(source.email, "@") {
		return avatarSource{}, false
	}
	return source, true
}

func avatarCommitHash(hash string) bool {
	if len(hash) < 7 || len(hash) > 64 || strings.Trim(hash, "0") == "" {
		return false
	}
	for _, r := range hash {
		if !(r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func (s avatarSource) key() string {
	if s.direct != "" {
		return s.origin + "\x00url:" + s.direct
	}
	identity := s.email
	if identity == "" {
		identity = s.project + "/" + s.hash
	}
	return s.origin + "\x00" + identity
}

func (s avatarSource) endpoint() (apiURL, endpoint, tool, host string) {
	u, _ := url.Parse(s.origin)
	host = u.Host
	if s.github {
		parts := strings.Split(s.project, "/")
		query := url.Values{"per_page": {"1"}}
		if s.email != "" {
			// An author's latest local commit may not have been pushed yet.
			// Published commits with the same email still identify their account.
			query.Set("author", s.email)
		} else {
			query.Set("sha", s.hash)
		}
		endpoint = "repos/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/commits?" + query.Encode()
		return "https://api.github.com/" + endpoint, endpoint, "gh", host
	}
	endpoint = "avatar?" + url.Values{"email": {s.email}, "size": {"96"}}.Encode()
	return s.origin + "/api/v4/" + endpoint, endpoint, "glab", host
}

type avatarEntry struct {
	bitmap    *ui.Bitmap
	digest    [32]byte
	seenFrame uint64
	pending   bool
	retryAt   time.Time
	used      time.Time
}

type authorAvatarStore struct {
	mu             sync.Mutex
	entries        map[string]*avatarEntry
	ctx            context.Context
	cancel         context.CancelFunc
	slots          chan struct{}
	directory      string
	client         *http.Client
	api            func(context.Context, string, string, string) ([]byte, error)
	now            func() time.Time
	frame          uint64
	visible        bool
	repaintPending bool
}

func newAuthorAvatarStore(directory string) *authorAvatarStore {
	ctx, cancel := context.WithCancel(context.Background())
	return &authorAvatarStore{
		entries: make(map[string]*avatarEntry), ctx: ctx, cancel: cancel, slots: make(chan struct{}, 4), directory: directory, visible: true,
		client: &http.Client{Timeout: 6 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 4 {
				return http.ErrUseLastResponse
			}
			if req.URL.Scheme != "https" && req.URL.Scheme != "http" || req.URL.User != nil {
				return http.ErrUseLastResponse
			}
			// Never downgrade an HTTPS image or API lookup.
			if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
				return http.ErrUseLastResponse
			}
			return nil
		}}, api: readAvatarHostingAPI, now: time.Now,
	}
}

// beginFrame retires subscriptions from the previous visible page and allows
// one repaint request for the new frame. Only lookups built in this frame may
// repaint it; an offscreen result can still populate the shared cache.
func (s *authorAvatarStore) beginFrame(visible bool) {
	s.mu.Lock()
	s.frame++
	s.visible = visible
	s.repaintPending = false
	s.mu.Unlock()
}

// lookup is safe during rendering: each identity has at most one guarded job.
// Workers own I/O and only publish immutable bitmaps under the store's lock.
// They retain Services.Invalidate, never a Context, Element or app state.
func (s *authorAvatarStore) lookup(source avatarSource, invalidate func()) *ui.Bitmap {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return nil
	}
	now := s.now()
	key := source.key()
	entry := s.entries[key]
	if entry == nil {
		if len(s.entries) >= avatarMemoryLimit {
			oldestKey := ""
			var oldest time.Time
			for candidate, value := range s.entries {
				if !value.pending && (oldestKey == "" || value.used.Before(oldest)) {
					oldestKey, oldest = candidate, value.used
				}
			}
			if oldestKey == "" {
				return nil
			}
			delete(s.entries, oldestKey)
		}
		entry = &avatarEntry{}
		s.entries[key] = entry
	}
	entry.used = now
	entry.seenFrame = s.frame
	if !entry.pending && !now.Before(entry.retryAt) {
		entry.pending = true
		go s.load(source, key, entry, invalidate)
	}
	return entry.bitmap
}

func (s *authorAvatarStore) load(source avatarSource, key string, entry *avatarEntry, invalidate func()) {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-s.ctx.Done():
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	publish := func(bitmap *ui.Bitmap, encoded []byte, done bool, retryAt time.Time) {
		s.mu.Lock()
		active := s.ctx.Err() == nil && s.entries[key] == entry
		changed := false
		if active {
			if bitmap != nil && (entry.bitmap == nil || entry.digest != sha256.Sum256(encoded)) {
				entry.bitmap = bitmap
				entry.digest = sha256.Sum256(encoded)
				changed = true
			}
			if done {
				entry.pending = false
				entry.retryAt = retryAt
			}
		}
		repaint := active && changed && s.visible && entry.seenFrame == s.frame && !s.repaintPending && invalidate != nil
		if repaint {
			s.repaintPending = true
		}
		s.mu.Unlock()
		if repaint {
			invalidate()
		}
	}
	path := ""
	if s.directory != "" {
		path = filepath.Join(s.directory, fmt.Sprintf("%x.png", sha256.Sum256([]byte(key))))
	}
	if path != "" {
		if data, err := readAvatarFile(path); err == nil {
			if bitmap, encoded, err := decodeAuthorAvatar(data); err == nil {
				if info, err := os.Stat(path); err == nil {
					freshUntil := info.ModTime().Add(avatarFreshFor)
					if s.now().Before(freshUntil) {
						publish(bitmap, encoded, true, freshUntil)
						return
					}
					// Stale cached photos remain usable offline while a refresh runs.
					publish(bitmap, encoded, false, time.Time{})
				}
			}
		}
	}
	bitmap, data := s.fetch(ctx, source)
	if bitmap != nil && ctx.Err() == nil && path != "" {
		writeAvatarFile(path, data)
	}
	retryAt := s.now().Add(avatarRetryAfter)
	if bitmap != nil {
		retryAt = s.now().Add(avatarFreshFor)
	}
	publish(bitmap, data, true, retryAt)
}

func (s *authorAvatarStore) fetch(ctx context.Context, source avatarSource) (*ui.Bitmap, []byte) {
	if source.direct != "" {
		data, err := s.get(ctx, source.direct, avatarImageLimit)
		if err != nil {
			return nil, nil
		}
		bitmap, encoded, err := decodeAuthorAvatar(data)
		if err != nil {
			return nil, nil
		}
		return bitmap, encoded
	}
	apiURL, endpoint, tool, host := source.endpoint()
	body, err := s.get(ctx, apiURL, 512<<10)
	if err != nil && ctx.Err() == nil && s.api != nil {
		body, err = s.api(ctx, tool, host, endpoint)
	}
	if err != nil {
		return nil, nil
	}
	avatarURL := ""
	if source.github {
		var commits []struct {
			SHA    string `json:"sha"`
			Commit struct {
				Author struct {
					Email string `json:"email"`
				} `json:"author"`
			} `json:"commit"`
			Author *struct {
				URL string `json:"avatar_url"`
			} `json:"author"`
		}
		if json.Unmarshal(body, &commits) != nil || len(commits) == 0 || commits[0].Author == nil {
			return nil, nil
		}
		if source.email != "" && !strings.EqualFold(commits[0].Commit.Author.Email, source.email) {
			return nil, nil
		}
		if source.email == "" && !strings.HasPrefix(commits[0].SHA, source.hash) {
			return nil, nil
		}
		avatarURL = commits[0].Author.URL
	} else {
		var author struct {
			URL string `json:"avatar_url"`
		}
		if json.Unmarshal(body, &author) != nil {
			return nil, nil
		}
		avatarURL = author.URL
	}
	avatarURL = normalizeAvatarURL(source.origin, avatarURL)
	if avatarURL == "" {
		return nil, nil
	}
	data, err := s.get(ctx, avatarURL, avatarImageLimit)
	if err != nil {
		return nil, nil
	}
	bitmap, encoded, err := decodeAuthorAvatar(data)
	if err != nil {
		return nil, nil
	}
	return bitmap, encoded
}

// Authentication stays with the existing hosting CLI. No login, credential
// prompt, shell, repository command log or user-facing error is involved.
func readAvatarHostingAPI(ctx context.Context, tool, host, endpoint string) ([]byte, error) {
	// Reuse the tool discovery fallback for Finder-launched macOS apps.
	program, err := findLanguageServerProgram(tool, currentLanguageServerToolDirectories())
	if err != nil {
		return nil, err
	}
	command := exec.CommandContext(ctx, program, "api", "--hostname", host, endpoint)
	command.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GLAB_CHECK_UPDATE=false", "NO_COLOR=1")
	command.Stderr = io.Discard
	command.WaitDelay = 500 * time.Millisecond
	output := &avatarLimitedBuffer{limit: 512 << 10}
	command.Stdout = output
	if err := command.Run(); err != nil {
		return nil, err
	}
	return output.buffer.Bytes(), nil
}

type avatarLimitedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *avatarLimitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.buffer.Len() {
		return 0, io.ErrShortWrite
	}
	return b.buffer.Write(p)
}

func (s *authorAvatarStore) get(ctx context.Context, address string, limit int64) ([]byte, error) {
	u, err := url.Parse(address)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, fmt.Errorf("invalid avatar URL")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "lazygitGo")
	response, err := s.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("avatar unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, fmt.Errorf("avatar response unavailable")
	}
	return data, nil
}

func normalizeAvatarURL(origin, value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	base, err := url.Parse(origin + "/")
	if err != nil {
		return ""
	}
	u, err := base.Parse(value)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	if base.Scheme == "https" && u.Scheme != "https" {
		return ""
	}
	// GitLab may return a Gravatar default when no real account photo exists.
	// Ask for a 404 instead of replacing our initial with an identicon.
	host := strings.ToLower(u.Hostname())
	if host == "gravatar.com" || strings.HasSuffix(host, ".gravatar.com") || host == "libravatar.org" || strings.HasSuffix(host, ".libravatar.org") {
		query := u.Query()
		query.Set("d", "404")
		query.Set("s", "96")
		u.RawQuery = query.Encode()
	}
	if strings.Contains(u.Path, "/assets/no_avatar") {
		return ""
	}
	return u.String()
}

func decodeAuthorAvatar(data []byte) (*ui.Bitmap, []byte, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width) > 4_000_000/int64(config.Height) {
		return nil, nil, fmt.Errorf("invalid avatar image")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	thumb := image.NewRGBA(image.Rect(0, 0, 96, 96))
	// Crop to a square before scaling so photos never stretch in round avatars.
	bounds := source.Bounds()
	side := min(bounds.Dx(), bounds.Dy())
	start := image.Pt(bounds.Min.X+(bounds.Dx()-side)/2, bounds.Min.Y+(bounds.Dy()-side)/2)
	draw.CatmullRom.Scale(thumb, thumb.Bounds(), source, image.Rectangle{Min: start, Max: start.Add(image.Pt(side, side))}, draw.Src, nil)
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, thumb); err != nil {
		return nil, nil, err
	}
	return ui.NewBitmap(thumb), encoded.Bytes(), nil
}

func readAvatarFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, avatarImageLimit+1))
	if err != nil || len(data) > avatarImageLimit {
		return nil, fmt.Errorf("invalid avatar cache")
	}
	return data, nil
}

func writeAvatarFile(path string, data []byte) {
	directory := filepath.Dir(path)
	if os.MkdirAll(directory, 0700) != nil {
		return
	}
	file, err := os.CreateTemp(directory, ".avatar-")
	if err != nil {
		return
	}
	defer os.Remove(file.Name())
	_, err = file.Write(data)
	if closeErr := file.Close(); err != nil || closeErr != nil {
		return
	}
	if os.Rename(file.Name(), path) != nil {
		return
	}
	// Bound persistent storage as well as memory. Only our image files count.
	entries, err := os.ReadDir(directory)
	if err != nil {
		return
	}
	type cached struct {
		path     string
		modified time.Time
	}
	var images []cached
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".png") {
			if info, err := entry.Info(); err == nil {
				images = append(images, cached{filepath.Join(directory, entry.Name()), info.ModTime()})
			}
		}
	}
	if len(images) <= avatarDiskLimit {
		return
	}
	sort.Slice(images, func(i, j int) bool { return images[i].modified.Before(images[j].modified) })
	for _, image := range images[:len(images)-avatarDiskLimit] {
		_ = os.Remove(image.path)
	}
}
