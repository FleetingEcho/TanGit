package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// overviewRepositoriesFile keeps the set of repositories the Overview page
// follows. It lives beside the recent history, but is its own file: the
// Overview set is chosen by the user, not by what was opened last, so it never
// falls off the history's cap.
const overviewRepositoriesFile = "overview-repositories.json"

// Overview time windows, in days, the page offers.
var overviewRanges = [...]int{1, 3, 7, 14, 30}

const overviewDefaultRange = 1 // Three days.

type overviewPeriod struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

func (p overviewPeriod) valid() bool {
	return !p.From.IsZero() && !p.To.IsZero() && !p.From.After(p.To)
}

// overviewRepo is one followed repository. Custom marks a folder the user
// added by hand, as opposed to one picked from the recent history, so the
// editing dialog can list it under its own heading.
type overviewRepo struct {
	Path   string `json:"path"`
	Name   string `json:"name,omitempty"`
	Custom bool   `json:"custom,omitempty"`
}

func (r overviewRepo) displayName() string {
	if name := strings.TrimSpace(r.Name); name != "" {
		return name
	}
	return repoName(r.Path)
}

// overviewDef is one named Overview: a set of repositories and the time window
// its page was last left on. A user can keep several, for example one per
// project or team, and switch between them from the page's header.
type overviewDef struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Repos []overviewRepo `json:"repos"`
	// Days is the time window the user last chose; 0 means the default.
	Days   int             `json:"days,omitempty"`
	Period *overviewPeriod `json:"period,omitempty"`
}

// overviewConfig is what the Overview page remembers between starts.
type overviewConfig struct {
	Overviews []overviewDef `json:"overviews"`
	// Active is the ID of the Overview the page showed last.
	Active string `json:"active,omitempty"`

	// LegacyRepos and LegacyDays read the first version of the file, which held
	// a single unnamed set. They are migrated on load and never written back.
	LegacyRepos []overviewRepo `json:"repos,omitempty"`
	LegacyDays  int            `json:"days,omitempty"`
}

// current is the Overview on show: the active one, or the first while Active
// names nothing (a stale ID). It is nil when there are none.
func (c *overviewConfig) current() *overviewDef {
	for i := range c.Overviews {
		if c.Overviews[i].ID == c.Active {
			return &c.Overviews[i]
		}
	}
	if len(c.Overviews) > 0 {
		return &c.Overviews[0]
	}
	return nil
}

func (c *overviewConfig) byID(id string) *overviewDef {
	for i := range c.Overviews {
		if c.Overviews[i].ID == id {
			return &c.Overviews[i]
		}
	}
	return nil
}

// newOverviewID makes an ID no existing Overview holds.
func newOverviewID(existing []overviewDef) string {
	for n := time.Now().UnixNano(); ; n++ {
		id := strconv.FormatInt(n, 36)
		taken := false
		for _, def := range existing {
			taken = taken || def.ID == id
		}
		if !taken {
			return id
		}
	}
}

// overviewNameLimit bounds a name, in characters, so it cannot outgrow the
// header it is drawn in.
const overviewNameLimit = 40

// cleanOverviewName trims a name and keeps it to the limit.
func cleanOverviewName(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	if runes := []rune(name); len(runes) > overviewNameLimit {
		name = strings.TrimSpace(string(runes[:overviewNameLimit]))
	}
	return name
}

// uniqueOverviewName returns name, or name with a number, so two Overviews
// never read the same in the dropdown. except is the ID being renamed.
func uniqueOverviewName(defs []overviewDef, name, except string) string {
	taken := func(candidate string) bool {
		for _, def := range defs {
			if def.ID != except && strings.EqualFold(def.Name, candidate) {
				return true
			}
		}
		return false
	}
	if !taken(name) {
		return name
	}
	for n := 2; ; n++ {
		candidate := name + " " + strconv.Itoa(n)
		if !taken(candidate) {
			return candidate
		}
	}
}

func overviewStorePath(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, overviewRepositoriesFile)
}

// loadOverviewConfig reads the stored set. It never fails the app: a missing
// or damaged file is an empty set, which makes the page ask what to follow.
func loadOverviewConfig(dir string) overviewConfig {
	path := overviewStorePath(dir)
	if path == "" {
		return overviewConfig{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return overviewConfig{}
	}
	var config overviewConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return overviewConfig{}
	}
	if len(config.Overviews) == 0 && len(config.LegacyRepos) > 0 {
		config.Overviews = []overviewDef{{
			ID: newOverviewID(nil), Name: tr("My repositories"),
			Repos: config.LegacyRepos, Days: config.LegacyDays,
		}}
	}
	config.LegacyRepos, config.LegacyDays = nil, 0
	for i := range config.Overviews {
		config.Overviews[i].Repos = normalizeOverviewRepos(config.Overviews[i].Repos)
		if config.Overviews[i].ID == "" {
			config.Overviews[i].ID = newOverviewID(config.Overviews)
		}
		if strings.TrimSpace(config.Overviews[i].Name) == "" {
			config.Overviews[i].Name = tr("Overview")
		}
	}
	return config
}

// saveOverviewConfig writes the set atomically, as the recent history does: a
// crash mid-write leaves the last good file rather than a truncated one.
func saveOverviewConfig(dir string, config overviewConfig) error {
	path := overviewStorePath(dir)
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	config.LegacyRepos, config.LegacyDays = nil, 0
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// normalizeOverviewRepos drops blanks and repeats, keeping the first of each
// path (as written, so a remote reference stays untouched).
func normalizeOverviewRepos(repos []overviewRepo) []overviewRepo {
	seen := make(map[string]bool, len(repos))
	out := make([]overviewRepo, 0, len(repos))
	for _, repo := range repos {
		repo.Path = strings.TrimSpace(repo.Path)
		if repo.Path == "" || seen[repo.Path] {
			continue
		}
		seen[repo.Path] = true
		out = append(out, repo)
	}
	return out
}
