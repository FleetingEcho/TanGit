package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type languageServerInstallation struct {
	Path           string
	Arguments      []string
	ProbeArguments []string
}

var performLanguageServerInstall = installLanguageServerTool

// Node-based servers share one managed prefix, so installs must not mutate
// its package manifest and node_modules tree concurrently.
var languageServerInstallMu sync.Mutex

var runLanguageServerInstallCommand = func(ctx context.Context, program string, arguments, environment []string) ([]byte, error) {
	command := exec.CommandContext(ctx, program, arguments...)
	command.Env = languageServerCommandEnvironment(command.Environ(), program, environment)
	return command.CombinedOutput()
}

func (a *app) offerLanguageServerInstall(path string) {
	for _, profile := range languageServerProfiles {
		if profile.Custom {
			continue
		}
		if _, matches := languageServerProfileForPath(profile, path); !matches {
			continue
		}
		server, configured := a.settings.LanguageServers[profile.ID]
		if server.Disabled || a.languageServerInstallPrompted[profile.ID] || a.languageServerExecutableAvailable(profile) {
			continue
		}
		// TypeScript and Rust keep their first-use offer. Other servers are
		// offered only after the user enabled them (or when enabled by default),
		// so one JavaScript file does not advertise every optional analyzer.
		if !configured && profile.ID != "typescript" && profile.ID != "rust" && !profile.EnabledByDefault {
			continue
		}
		if a.languageServerInstallPrompted == nil {
			a.languageServerInstallPrompted = make(map[string]bool)
		}
		a.languageServerInstallPrompted[profile.ID] = true
		id, name := profile.ID, profile.Name
		a.pendingToasts = append(a.pendingToasts, appToast{
			kind: "info", title: trf("Install %s?", name),
			description: trf("Download and configure code intelligence for %s files.", filepath.Ext(path)),
			action:      tr("Download"), onAction: func() { a.installLanguageServer(id) }, timeout: 12 * time.Second,
		})
	}
}

func (a *app) installLanguageServer(id string) {
	profile, ok := languageServerProfileByID(id)
	if !ok || a.languageServerInstalling[id] {
		return
	}
	dataDir := a.dataDir()
	if dataDir == "" {
		a.notifyTranslated("error", trf("Could not install %s", profile.Name), "TanGit's user data directory is unavailable.")
		return
	}
	if a.languageServerInstalling == nil {
		a.languageServerInstalling = make(map[string]bool)
	}
	a.languageServerInstalling[id] = true
	a.notifyTranslated("info", trf("Installing %s", profile.Name), "Downloading to TanGit's managed tools directory…")
	go func() {
		defer a.recoverBackground()
		var installation languageServerInstallation
		var err error
		func() {
			languageServerInstallMu.Lock()
			defer languageServerInstallMu.Unlock()
			installation, err = performLanguageServerInstall(profile, dataDir)
		}()
		a.update(func() {
			delete(a.languageServerInstalling, id)
			if err != nil {
				a.notify("error", trf("Could not install %s", profile.Name), safeError(err))
				return
			}
			if a.settings.LanguageServers == nil {
				a.settings.LanguageServers = make(map[string]languageServerSettings)
			}
			a.settings.LanguageServers[id] = languageServerSettings{
				Path: installation.Path, Arguments: append([]string(nil), installation.Arguments...),
				ProbeArguments: append([]string(nil), installation.ProbeArguments...),
			}
			if id == goLanguageServerID {
				a.stopGoLanguageServer()
			} else if server := a.additionalLanguageServers[id]; server != nil {
				a.stopManagedLanguageServer(server)
				delete(a.additionalLanguageServers, id)
			}
			a.saveSettings()
			a.loadGoLanguageServerDraft()
			a.notifyTranslated("success", trf("%s installed", profile.Name), "Code intelligence is now enabled for matching files.")
		})
	}()
}

func installLanguageServerTool(profile languageServerProfile, dataDir string) (languageServerInstallation, error) {
	toolsDir := filepath.Join(dataDir, "tools")
	if err := os.MkdirAll(toolsDir, 0o755); err != nil {
		return languageServerInstallation{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	switch profile.ID {
	case goLanguageServerID:
		goTool, err := languageServerLookPath("go")
		if err != nil {
			return languageServerInstallation{}, errors.New("Go is required to download gopls")
		}
		binDir := filepath.Join(toolsDir, "bin")
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			return languageServerInstallation{}, err
		}
		if err := runLanguageServerInstaller(ctx, goTool, []string{"install", "golang.org/x/tools/gopls@latest"}, []string{"GOBIN=" + binDir}); err != nil {
			return languageServerInstallation{}, err
		}
		return languageServerInstallation{Path: filepath.Join(binDir, executableName("gopls")), ProbeArguments: append([]string(nil), profile.VersionArguments...)}, nil
	case "rust":
		rustup, err := languageServerLookPath("rustup")
		if err != nil {
			return languageServerInstallation{}, errors.New("rustup is required to install rust-analyzer")
		}
		if err := runLanguageServerInstaller(ctx, rustup, []string{"component", "add", "rust-analyzer"}, nil); err != nil {
			return languageServerInstallation{}, err
		}
		path, err := languageServerLookPath("rust-analyzer")
		if err != nil {
			return languageServerInstallation{}, errors.New("rust-analyzer was installed but is not available on PATH")
		}
		return languageServerInstallation{Path: path, ProbeArguments: append([]string(nil), profile.VersionArguments...)}, nil
	case "typescript":
		return installNodeLanguageServer(ctx, toolsDir, profile, "typescript")
	case "html", "css", "json", "eslint":
		return installNodeLanguageServer(ctx, toolsDir, profile, "@t1ckbase/vscode-langservers-extracted")
	case "tailwind":
		return installNodeLanguageServer(ctx, toolsDir, profile, "@tailwindcss/language-server")
	case "oxlint":
		return installNodeLanguageServer(ctx, toolsDir, profile, "oxlint")
	case "basedpyright":
		return installNodeLanguageServer(ctx, toolsDir, profile, "basedpyright")
	case "yaml":
		return installNodeLanguageServer(ctx, toolsDir, profile, "yaml-language-server")
	case "vue":
		return installNodeLanguageServer(ctx, toolsDir, profile, "@vue/language-server")
	case "svelte":
		return installNodeLanguageServer(ctx, toolsDir, profile, "svelte-language-server")
	case "astro":
		return installNodeLanguageServer(ctx, toolsDir, profile, "@astrojs/language-server")
	case "bash":
		return installNodeLanguageServer(ctx, toolsDir, profile, "bash-language-server")
	case "docker":
		return installNodeLanguageServer(ctx, toolsDir, profile, "dockerfile-language-server-nodejs")
	case "ty", "ruff":
		return installPythonLanguageServer(ctx, toolsDir, profile, profile.ID)
	case "marksman":
		return installMarksman(ctx, toolsDir)
	default:
		return languageServerInstallation{}, fmt.Errorf("no managed installer for %s", profile.Name)
	}
}

func installPythonLanguageServer(ctx context.Context, toolsDir string, profile languageServerProfile, packageName string) (languageServerInstallation, error) {
	python, err := findFirstExecutable("python3", "python")
	if err != nil {
		return languageServerInstallation{}, errors.New("Python is required to download " + profile.Name)
	}
	venv := filepath.Join(toolsDir, "python")
	venvPython := filepath.Join(venv, "bin", "python")
	venvProgram := filepath.Join(venv, "bin", profile.Program)
	if runtime.GOOS == "windows" {
		venvPython = filepath.Join(venv, "Scripts", "python.exe")
		venvProgram = filepath.Join(venv, "Scripts", profile.Program+".exe")
	}
	if _, err := os.Stat(venvPython); err != nil {
		if err := runLanguageServerInstaller(ctx, python, []string{"-m", "venv", venv}, nil); err != nil {
			return languageServerInstallation{}, err
		}
	}
	if err := runLanguageServerInstaller(ctx, venvPython, []string{"-m", "pip", "install", "--upgrade", packageName}, nil); err != nil {
		return languageServerInstallation{}, err
	}
	if _, err := os.Stat(venvProgram); err != nil {
		return languageServerInstallation{}, fmt.Errorf("find installed %s executable: %w", packageName, err)
	}
	return languageServerInstallation{
		Path: venvProgram, Arguments: append([]string(nil), profile.Arguments...),
		ProbeArguments: append([]string(nil), profile.VersionArguments...),
	}, nil
}

func findFirstExecutable(names ...string) (string, error) {
	for _, name := range names {
		if path, err := languageServerLookPath(name); err == nil {
			return path, nil
		}
	}
	return "", exec.ErrNotFound
}

func installMarksman(ctx context.Context, toolsDir string) (languageServerInstallation, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/artempyanykh/marksman/releases/latest", nil)
	if err != nil {
		return languageServerInstallation{}, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "TanGit language-server installer")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return languageServerInstallation{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return languageServerInstallation{}, fmt.Errorf("find Marksman release: %s", response.Status)
	}
	var release struct {
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2<<20)).Decode(&release); err != nil {
		return languageServerInstallation{}, err
	}
	want := "linux"
	if runtime.GOOS == "windows" {
		want = "windows"
	} else if runtime.GOOS == "darwin" {
		want = "macos"
	}
	assetURL := ""
	for _, asset := range release.Assets {
		name := strings.ToLower(asset.Name)
		if strings.Contains(name, want) && !strings.HasSuffix(name, ".sha256") && !strings.HasSuffix(name, ".txt") {
			assetURL = asset.URL
			break
		}
	}
	if assetURL == "" {
		return languageServerInstallation{}, fmt.Errorf("the latest Marksman release has no %s binary", want)
	}
	binDir := filepath.Join(toolsDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return languageServerInstallation{}, err
	}
	target := filepath.Join(binDir, executableName("marksman"))
	if err := downloadLanguageServerBinary(ctx, assetURL, target); err != nil {
		return languageServerInstallation{}, err
	}
	return languageServerInstallation{Path: target, ProbeArguments: []string{"--version"}}, nil
}

func downloadLanguageServerBinary(ctx context.Context, url, target string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "TanGit language-server installer")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("download language server: %s", response.Status)
	}
	if response.ContentLength > 100<<20 {
		return errors.New("language-server download is larger than 100 MB")
	}
	temporary := target + ".download"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, io.LimitReader(response.Body, 100<<20+1))
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(temporary)
		return errors.Join(copyErr, closeErr)
	}
	info, err := os.Stat(temporary)
	if err != nil || info.Size() > 100<<20 {
		_ = os.Remove(temporary)
		return errors.New("language-server download is larger than 100 MB")
	}
	if err := os.Chmod(temporary, 0o755); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, target); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func installNodeLanguageServer(ctx context.Context, toolsDir string, profile languageServerProfile, packages ...string) (languageServerInstallation, error) {
	npm, err := languageServerLookPath("npm")
	if err != nil {
		return languageServerInstallation{}, errors.New("Node.js and npm are required to download this language server")
	}
	node, err := languageServerLookPath("node")
	if err != nil {
		return languageServerInstallation{}, errors.New("Node.js is required to run this language server")
	}
	prefix := filepath.Join(toolsDir, "node")
	arguments := []string{"install", "--no-audit", "--no-fund", "--prefix", prefix}
	for _, name := range packages {
		version := "@latest"
		if name == "typescript" {
			// TypeScript 7 ships its own native LSP. Pin the major so a future
			// compiler release cannot silently change TanGit's protocol backend.
			version = "@7"
		}
		arguments = append(arguments, name+version)
	}
	if err := runLanguageServerInstaller(ctx, npm, arguments, nil); err != nil {
		return languageServerInstallation{}, err
	}
	binPath, err := nodePackageExecutable(prefix, packages[0], profile.Program)
	if err != nil {
		return languageServerInstallation{}, err
	}
	return languageServerInstallation{
		Path: node, Arguments: append([]string{binPath}, profile.Arguments...),
		ProbeArguments: append([]string{binPath}, profile.VersionArguments...),
	}, nil
}

func nodePackageExecutable(prefix, packageName, preferred string) (string, error) {
	packageDir := filepath.Join(prefix, "node_modules", filepath.FromSlash(packageName))
	data, err := os.ReadFile(filepath.Join(packageDir, "package.json"))
	if err != nil {
		return "", fmt.Errorf("read installed %s package: %w", packageName, err)
	}
	var manifest struct {
		Bin json.RawMessage `json:"bin"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return "", fmt.Errorf("read installed %s package metadata", packageName)
	}
	var relative string
	if json.Unmarshal(manifest.Bin, &relative) != nil {
		var binaries map[string]string
		if json.Unmarshal(manifest.Bin, &binaries) != nil {
			return "", fmt.Errorf("installed %s package has no executable", packageName)
		}
		relative = binaries[preferred]
		if relative == "" && len(binaries) == 1 {
			for _, candidate := range binaries {
				relative = candidate
			}
		}
	}
	if strings.TrimSpace(relative) == "" {
		return "", fmt.Errorf("installed %s package has no %s executable", packageName, preferred)
	}
	path := filepath.Clean(filepath.Join(packageDir, filepath.FromSlash(relative)))
	inside, err := filepath.Rel(packageDir, path)
	if err != nil || inside == ".." || strings.HasPrefix(inside, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("installed %s package has an invalid executable path", packageName)
	}
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("find installed %s executable: %w", packageName, err)
	}
	return path, nil
}

func runLanguageServerInstaller(ctx context.Context, program string, arguments, environment []string) error {
	output, err := runLanguageServerInstallCommand(ctx, program, arguments, environment)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.New("language-server download timed out after 5 minutes")
	}
	if err != nil {
		detail := strings.TrimSpace(string(output))
		if len(detail) > 600 {
			detail = detail[len(detail)-600:]
		}
		if detail != "" {
			return fmt.Errorf("%s: %w", detail, err)
		}
		return err
	}
	return nil
}

func executableName(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}
