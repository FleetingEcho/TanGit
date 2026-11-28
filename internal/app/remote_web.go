package app

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/FleetingEcho/lazygitGo/internal/repository"
)

// normalizeRemoteWebURL accepts only browser destinations. In particular, an
// override must not turn a remote row into a launcher for another URL scheme,
// and browser URLs never need to carry Git credentials.
func normalizeRemoteWebURL(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	for _, r := range value {
		if r < ' ' || r == 0x7f {
			return "", false
		}
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" || parsed.User != nil {
		return "", false
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", false
	}
	return parsed.String(), true
}

// deriveRemoteWebURL converts the common Git transport forms to the
// repository's browser page. Local paths and unrecognized transports return an
// empty string so the user can provide the web address explicitly.
func deriveRemoteWebURL(remoteURL string) string {
	remoteURL = strings.TrimSpace(remoteURL)
	if remoteURL == "" {
		return ""
	}
	if parsed, err := url.Parse(remoteURL); err == nil && parsed.Scheme != "" {
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https":
			parsed.Path = remoteWebRepositoryPath(parsed.Path)
			parsed.RawPath = ""
			parsed.RawQuery, parsed.Fragment = "", ""
			if webURL, ok := normalizeRemoteWebURL(parsed.String()); ok {
				return webURL
			}
			return ""
		case "ssh", "git", "git+ssh":
			host := parsed.Hostname()
			if host == "" || parsed.Path == "" {
				return ""
			}
			// An SSH port belongs to Git transport, not to the hosting site's
			// web server, so derived links retain only the host.
			if strings.Contains(host, ":") {
				host = "[" + host + "]"
			}
			web := url.URL{Scheme: "https", Host: host, Path: remoteWebRepositoryPath(parsed.Path)}
			if webURL, ok := normalizeRemoteWebURL(web.String()); ok {
				return webURL
			}
		}
		return ""
	}

	// Git's scp-like syntax has no URL scheme: [user@]host:path. Require a
	// user component so an ordinary local path is never mistaken for a host.
	at := strings.LastIndex(remoteURL, "@")
	if at < 0 {
		return ""
	}
	host, path, ok := strings.Cut(remoteURL[at+1:], ":")
	if !ok || host == "" || path == "" || strings.ContainsAny(host, "/\\") {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	web := url.URL{Scheme: "https", Host: host, Path: remoteWebRepositoryPath(path)}
	webURL, _ := normalizeRemoteWebURL(web.String())
	return webURL
}

func remoteWebRepositoryPath(path string) string {
	path = strings.TrimRight(path, "/")
	return strings.TrimSuffix(path, ".git")
}

func (a *app) remoteWebRepositoryKey() string {
	if home := strings.TrimSpace(a.remoteWebHomePath); home != "" {
		return filepath.Clean(home)
	}
	root := strings.TrimSpace(a.snapshot.Root)
	if root == "" {
		root = strings.TrimSpace(a.path)
	}
	if root == "" {
		return ""
	}
	return filepath.Clean(root)
}

func normalizeRemoteWebURLs(overrides map[string]map[string]string) map[string]map[string]string {
	if len(overrides) == 0 {
		return nil
	}
	normalized := make(map[string]map[string]string, len(overrides))
	for root, remotes := range overrides {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		root = filepath.Clean(root)
		for name, value := range remotes {
			name = strings.TrimSpace(name)
			webURL, ok := normalizeRemoteWebURL(value)
			if name == "" || !ok {
				continue
			}
			if normalized[root] == nil {
				normalized[root] = make(map[string]string)
			}
			normalized[root][name] = webURL
		}
	}
	for root, remotes := range normalized {
		if len(remotes) == 0 {
			delete(normalized, root)
		}
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

// remoteWebAddress returns the explicit choice for this repository and remote, or
// derives one from its fetch URLs. It never returns an unsafe stored value,
// including while an unnormalized settings fixture is under test.
func (a *app) remoteWebAddress(remote repository.Remote) string {
	if remotes := a.settings.RemoteWebURLs[a.remoteWebRepositoryKey()]; remotes != nil {
		if webURL, ok := normalizeRemoteWebURL(remotes[remote.Name]); ok {
			return webURL
		}
	}
	for _, remoteURL := range remote.URLs {
		if webURL := deriveRemoteWebURL(remoteURL); webURL != "" {
			return webURL
		}
	}
	return ""
}

func (a *app) openRemoteWebURL(services ui.Services, remote repository.Remote) {
	a.openWebPage(services, a.remoteWebAddress(remote))
}

// remoteWebProbe reports whether the web host answers at all. It is a variable
// so tests never touch the network.
var remoteWebProbe = probeRemoteWeb

const remoteWebProbeTTL = 5 * time.Minute

// probeRemoteWeb treats any HTTP response, including a login redirect, 401 or
// 404, as reachable: a private repository answers that way. Only a failure to
// connect (DNS, refused, TLS, timeout) means the address is wrong or offline.
func probeRemoteWeb(ctx context.Context, webURL string) error {
	client := &http.Client{
		Timeout:       4 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, webURL, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	return response.Body.Close()
}

// openWebPage opens a remote's web page after checking the host answers. An
// address that does not answer raises a notification that leads to Git hosting
// settings, where it can be corrected, instead of opening a browser error page.
func (a *app) openWebPage(services ui.Services, webURL string) {
	if webURL == "" {
		return
	}
	open := func() { services.OpenURL(webURL) }
	parsed, err := url.Parse(webURL)
	if err != nil || parsed.Hostname() == "" {
		open()
		return
	}
	host := parsed.Host
	if checked, ok := a.webReachable[host]; ok && time.Since(checked) < remoteWebProbeTTL {
		open()
		return
	}
	probe := remoteWebProbe
	go func() {
		defer a.recoverBackground()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := probe(ctx, webURL)
		a.update(func() {
			if err == nil {
				if a.webReachable == nil {
					a.webReachable = make(map[string]time.Time)
				}
				a.webReachable[host] = time.Now()
				open()
				return
			}
			a.notifyToast(appToast{
				kind:        "error",
				title:       trf("Cannot reach %s", host),
				description: trf("Check the web address in “%s”, then try again.", tr("Git hosting settings")) + " " + safeError(err),
				action:      "Git hosting settings",
				onAction:    a.openHostingAccess,
				moreActions: []appToastAction{{label: "Open anyway", onAction: open}},
				timeout:     12 * time.Second,
			})
		})
	}()
}

func (a *app) copyRemoteWebURL(services ui.Services, remote repository.Remote) {
	if webURL := a.remoteWebAddress(remote); webURL != "" {
		a.notifyCopied(services, "web URL", webURL)
	}
}

// webRemote is the remote the status bar's web address button edits: origin,
// else the first one.
func (a *app) webRemote() (repository.Remote, bool) {
	for _, remote := range a.snapshot.Remotes {
		if remote.Name == "origin" {
			return remote, true
		}
	}
	if len(a.snapshot.Remotes) > 0 {
		return a.snapshot.Remotes[0], true
	}
	return repository.Remote{}, false
}

func (a *app) selectedRemoteForWeb() (repository.Remote, bool) {
	if a.selectedRemote < 0 || a.selectedRemote >= len(a.snapshot.Remotes) {
		return repository.Remote{}, false
	}
	return a.snapshot.Remotes[a.selectedRemote], true
}

func (a *app) openSelectedRemoteWebURL() {
	if remote, ok := a.selectedRemoteForWeb(); ok {
		a.openRemoteWebURL(a.services, remote)
	}
}

func (a *app) copySelectedRemoteWebURL() {
	if remote, ok := a.selectedRemoteForWeb(); ok {
		a.copyRemoteWebURL(a.services, remote)
	}
}

func (a *app) editSelectedRemoteWebURL() {
	if remote, ok := a.selectedRemoteForWeb(); ok {
		a.openEditRemoteWebURL(remote)
	}
}

func (a *app) openEditRemoteWebURL(remote repository.Remote) {
	a.remoteWebHomePath = ""
	a.remoteWebOriginalName = remote.Name
	a.remoteWebURL = a.remoteWebAddress(remote)
	a.remoteWebURLError = ""
	a.showRemoteCheckoutForm = false
	a.showRemoteWebURLForm = true
}

func (a *app) saveRemoteWebURLEdit() {
	root := a.remoteWebRepositoryKey()
	name := strings.TrimSpace(a.remoteWebOriginalName)
	webURL, ok := normalizeRemoteWebURL(a.remoteWebURL)
	if root == "" || name == "" {
		return
	}
	if !ok {
		a.remoteWebURLError = tr("Enter a valid HTTP(S) web URL without credentials.")
		return
	}
	remotes := make(map[string]string)
	for existingName, existingURL := range a.settings.RemoteWebURLs[root] {
		remotes[existingName] = existingURL
	}
	remotes[name] = webURL
	if a.settings.RemoteWebURLs == nil {
		a.settings.RemoteWebURLs = make(map[string]map[string]string)
	}
	a.settings.RemoteWebURLs[root] = remotes
	a.saveSettings()
	a.showRemoteWebURLForm = false
	a.remoteWebURLError = ""
	a.notify("success", trf("Saved web URL for %s", name), webURL)
}

// restoreRemoteWebURLOverride removes only the current remote's manual choice.
// Once it is gone, remoteWebAddress derives the destination from the Git remote
// again, including when different Git and web hostnames need a new override
// later.
func (a *app) restoreRemoteWebURLOverride() {
	root := a.remoteWebRepositoryKey()
	name := strings.TrimSpace(a.remoteWebOriginalName)
	remotes := a.settings.RemoteWebURLs[root]
	if root == "" || name == "" {
		return
	}
	if _, ok := remotes[name]; !ok {
		return
	}
	delete(remotes, name)
	a.settings.RemoteWebURLs = normalizeRemoteWebURLs(a.settings.RemoteWebURLs)
	a.saveSettings()

	automatic := ""
	for _, remote := range a.snapshot.Remotes {
		if remote.Name == name {
			automatic = a.remoteWebAddress(remote)
			break
		}
	}
	a.showRemoteWebURLForm = false
	a.remoteWebURLError = ""
	if automatic == "" {
		automatic = tr("No web URL could be derived.")
	}
	a.notify("success", trf("Restored automatic web URL for %s", name), automatic)
}

// moveRemoteWebURLOverride follows a successful remote rename. An empty new
// name removes the override after the remote itself has been removed.
func (a *app) moveRemoteWebURLOverride(oldName, newName string) {
	root := a.remoteWebRepositoryKey()
	remotes := a.settings.RemoteWebURLs[root]
	webURL, ok := remotes[oldName]
	if !ok {
		return
	}
	updated := make(map[string]string, len(remotes)+1)
	for name, value := range remotes {
		if name != oldName {
			updated[name] = value
		}
	}
	if newName != "" {
		updated[newName] = webURL
	}
	a.settings.RemoteWebURLs[root] = updated
	a.settings.RemoteWebURLs = normalizeRemoteWebURLs(a.settings.RemoteWebURLs)
	a.saveSettings()
}

func (a *app) remoteWebURLFormModal(c *ui.Context) {
	theme := c.Theme()
	_, canRestore := a.settings.RemoteWebURLs[a.remoteWebRepositoryKey()][a.remoteWebOriginalName]
	a.repositoryFormModal(c, &a.showRemoteWebURLForm, 600, "Edit web URL",
		"Use the repository page for any hosting provider.", externalLinkIcon, func() {
			ui.Column(c).Gap(10).Children(func() {
				ui.TextInput(c, &a.remoteWebURL).Label(tr("Remote web URL")).
					Placeholder(tr("https://example.com/team/project")).AutoFocus()
				ui.Text(c, tr("Only HTTP(S) addresses without credentials are allowed.")).
					TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
				ui.Text(c, trf("Remote: %s", a.remoteWebOriginalName)).
					TextColor(theme.TextMuted).FontSize(theme.FontSize - 1)
				if a.remoteWebURLError != "" {
					ui.Text(c, a.remoteWebURLError).TextColor(theme.Danger).FontSize(theme.FontSize - 1)
				}
			})
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				if ui.Button(c, tr("Restore automatic")).
					Tooltip(tr("Remove the saved override and derive the web URL from the Git remote.")).
					Disabled(!canRestore).Clicked() {
					a.restoreRemoteWebURLOverride()
				}
				ui.Spacer(c)
				if ui.Button(c, tr("Cancel")).Clicked() {
					a.showRemoteWebURLForm = false
				}
				if ui.PrimaryButton(c, tr("Save web URL")).
					Disabled(strings.TrimSpace(a.remoteWebURL) == "").Clicked() {
					a.saveRemoteWebURLEdit()
				}
			})
		})
}
