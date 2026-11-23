package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type languageServerExpansionRoundTripFunc func(*http.Request) (*http.Response, error)

func (f languageServerExpansionRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func languageServerExpansionResponse(status int, body []byte) *http.Response {
	return &http.Response{
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewReader(body)),
	}
}

func TestLanguageServerExpansionCatalogReleaseAssetSelection(t *testing.T) {
	tests := []struct {
		id, goos, goarch, tag, name string
	}{
		{id: "clangd", goos: "linux", goarch: "amd64", tag: "19.1.2", name: "clangd-linux-19.1.2.zip"},
		{id: "clangd", goos: "darwin", goarch: "amd64", tag: "19.1.2", name: "clangd-mac-19.1.2.zip"},
		{id: "clangd", goos: "windows", goarch: "amd64", tag: "19.1.2", name: "clangd-windows-19.1.2.zip"},
		{id: "lua", goos: "linux", goarch: "amd64", tag: "3.13.5", name: "lua-language-server-3.13.5-linux-x64.tar.gz"},
		{id: "lua", goos: "linux", goarch: "arm64", tag: "3.13.5", name: "lua-language-server-3.13.5-linux-arm64.tar.gz"},
		{id: "lua", goos: "darwin", goarch: "amd64", tag: "3.13.5", name: "lua-language-server-3.13.5-darwin-x64.tar.gz"},
		{id: "lua", goos: "darwin", goarch: "arm64", tag: "3.13.5", name: "lua-language-server-3.13.5-darwin-arm64.tar.gz"},
		{id: "lua", goos: "windows", goarch: "amd64", tag: "3.13.5", name: "lua-language-server-3.13.5-win32-x64.zip"},
		{id: "zls", goos: "linux", goarch: "amd64", tag: "0.14.0", name: "zls-x86_64-linux.tar.xz"},
		{id: "zls", goos: "linux", goarch: "arm64", tag: "0.14.0", name: "zls-aarch64-linux.tar.xz"},
		{id: "zls", goos: "darwin", goarch: "amd64", tag: "0.14.0", name: "zls-x86_64-macos.tar.xz"},
		{id: "zls", goos: "darwin", goarch: "arm64", tag: "0.14.0", name: "zls-aarch64-macos.tar.xz"},
		{id: "zls", goos: "windows", goarch: "amd64", tag: "0.14.0", name: "zls-x86_64-windows.zip"},
		{id: "zls", goos: "windows", goarch: "arm64", tag: "0.14.0", name: "zls-aarch64-windows.zip"},
		{id: "tombi", goos: "linux", goarch: "amd64", tag: "v0.4.8", name: "tombi-cli-0.4.8-x86_64-unknown-linux-musl.tar.gz"},
		{id: "tombi", goos: "linux", goarch: "arm64", tag: "v0.4.8", name: "tombi-cli-0.4.8-aarch64-unknown-linux-musl.tar.gz"},
		{id: "tombi", goos: "darwin", goarch: "amd64", tag: "v0.4.8", name: "tombi-cli-0.4.8-x86_64-apple-darwin.tar.gz"},
		{id: "tombi", goos: "darwin", goarch: "arm64", tag: "v0.4.8", name: "tombi-cli-0.4.8-aarch64-apple-darwin.tar.gz"},
		{id: "tombi", goos: "windows", goarch: "amd64", tag: "v0.4.8", name: "tombi-cli-0.4.8-x86_64-pc-windows-msvc.zip"},
		{id: "tombi", goos: "windows", goarch: "arm64", tag: "v0.4.8", name: "tombi-cli-0.4.8-aarch64-pc-windows-msvc.zip"},
		{id: "neocmake", goos: "linux", goarch: "amd64", tag: "0.8.20", name: "neocmakelsp-x86_64-unknown-linux-musl.tar.gz"},
		{id: "neocmake", goos: "linux", goarch: "arm64", tag: "0.8.20", name: "neocmakelsp-aarch64-unknown-linux-musl.tar.gz"},
		{id: "neocmake", goos: "darwin", goarch: "amd64", tag: "0.8.20", name: "neocmakelsp-universal-apple-darwin.tar.gz"},
		{id: "neocmake", goos: "darwin", goarch: "arm64", tag: "0.8.20", name: "neocmakelsp-universal-apple-darwin.tar.gz"},
		{id: "neocmake", goos: "windows", goarch: "amd64", tag: "0.8.20", name: "neocmakelsp-x86_64-pc-windows-msvc.zip"},
		{id: "neocmake", goos: "windows", goarch: "arm64", tag: "0.8.20", name: "neocmakelsp-aarch64-pc-windows-msvc.zip"},
		{id: "terraform", goos: "linux", goarch: "amd64", tag: "v0.36.5", name: "terraform-ls_0.36.5_linux_amd64.zip"},
		{id: "terraform", goos: "linux", goarch: "arm64", tag: "v0.36.5", name: "terraform-ls_0.36.5_linux_arm64.zip"},
		{id: "terraform", goos: "darwin", goarch: "amd64", tag: "v0.36.5", name: "terraform-ls_0.36.5_darwin_amd64.zip"},
		{id: "terraform", goos: "darwin", goarch: "arm64", tag: "v0.36.5", name: "terraform-ls_0.36.5_darwin_arm64.zip"},
		{id: "terraform", goos: "windows", goarch: "amd64", tag: "v0.36.5", name: "terraform-ls_0.36.5_windows_amd64.zip"},
		{id: "terraform", goos: "windows", goarch: "arm64", tag: "v0.36.5", name: "terraform-ls_0.36.5_windows_arm64.zip"},
	}
	for _, test := range tests {
		t.Run(test.id+"/"+test.goos+"/"+test.goarch, func(t *testing.T) {
			wantURL := "https://downloads.example/" + test.name
			release := languageServerRelease{Tag: test.tag, Assets: []languageServerReleaseAsset{
				{Name: "wrong-platform.zip", URL: "https://downloads.example/wrong-platform.zip"},
				{Name: test.name + ".sig", URL: "https://downloads.example/signature"},
				{Name: test.name, URL: "http://downloads.example/insecure"},
				{Name: test.name, URL: wantURL},
			}}
			asset, err := languageServerCatalogReleaseAsset(test.id, release, test.goos, test.goarch)
			if err != nil {
				t.Fatalf("select %s: %v", test.name, err)
			}
			if asset.Name != test.name || asset.URL != wantURL {
				t.Fatalf("selected asset = %#v, want %q at %q", asset, test.name, wantURL)
			}
		})
	}
}

func TestLanguageServerExpansionCatalogReleaseAssetsRejectUnsupportedAndInexactMatches(t *testing.T) {
	unsupported := []struct {
		id, goos, goarch string
	}{
		{id: "clangd", goos: "linux", goarch: "arm64"},
		{id: "clangd", goos: "darwin", goarch: "arm64"},
		{id: "clangd", goos: "windows", goarch: "arm64"},
		{id: "clangd", goos: "freebsd", goarch: "amd64"},
		{id: "lua", goos: "windows", goarch: "arm64"},
		{id: "lua", goos: "linux", goarch: "386"},
		{id: "zls", goos: "freebsd", goarch: "amd64"},
		{id: "tombi", goos: "linux", goarch: "386"},
		{id: "neocmake", goos: "linux", goarch: "386"},
		{id: "terraform", goos: "linux", goarch: "386"},
		{id: "unknown", goos: "linux", goarch: "amd64"},
	}
	for _, test := range unsupported {
		t.Run(test.id+"/"+test.goos+"/"+test.goarch, func(t *testing.T) {
			if _, err := languageServerCatalogReleaseAsset(test.id, languageServerRelease{Tag: "1.2.3"}, test.goos, test.goarch); err == nil {
				t.Fatal("unsupported platform or installer was accepted")
			}
		})
	}

	for _, id := range []string{"clangd", "lua", "zls", "tombi", "neocmake", "terraform"} {
		t.Run(id+" missing exact HTTPS archive", func(t *testing.T) {
			release := languageServerRelease{Tag: "1.2.3", Assets: []languageServerReleaseAsset{
				{Name: "unrelated.zip", URL: "https://downloads.example/unrelated.zip"},
				{Name: "release.zip.sig", URL: "https://downloads.example/release.zip.sig"},
				{Name: "release.zip", URL: "http://downloads.example/release.zip"},
			}}
			if _, err := languageServerCatalogReleaseAsset(id, release, "linux", "amd64"); err == nil {
				t.Fatal("missing archive was satisfied by a wrong, signature, or insecure asset")
			}
		})
	}
}

func TestLanguageServerExpansionTerraformCheckpoint(t *testing.T) {
	originalClient := languageServerReleaseHTTPClient
	t.Cleanup(func() { languageServerReleaseHTTPClient = originalClient })

	t.Run("valid", func(t *testing.T) {
		languageServerReleaseHTTPClient = &http.Client{Transport: languageServerExpansionRoundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Method != http.MethodGet || request.URL.String() != "https://checkpoint-api.hashicorp.com/v1/check/terraform-ls" {
				t.Fatalf("checkpoint request = %s %s", request.Method, request.URL)
			}
			if request.Header.Get("User-Agent") != "lazygitGo language-server installer" {
				t.Fatalf("checkpoint User-Agent = %q", request.Header.Get("User-Agent"))
			}
			return languageServerExpansionResponse(http.StatusOK, []byte(`{"current_version":"0.36.5"}`)), nil
		})}
		release, err := latestTerraformLanguageServerRelease(context.Background(), "linux", "arm64")
		if err != nil {
			t.Fatal(err)
		}
		wantName := "terraform-ls_0.36.5_linux_arm64.zip"
		wantURL := "https://releases.hashicorp.com/terraform-ls/0.36.5/" + wantName
		if release.Tag != "0.36.5" || len(release.Assets) != 1 || release.Assets[0].Name != wantName || release.Assets[0].URL != wantURL {
			t.Fatalf("checkpoint release = %#v", release)
		}
	})

	for _, test := range []struct {
		name, body string
	}{
		{name: "malformed JSON", body: `{"current_version":`},
		{name: "invalid semantic version", body: `{"current_version":"v0.36.5"}`},
		{name: "missing version", body: `{}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			languageServerReleaseHTTPClient = &http.Client{Transport: languageServerExpansionRoundTripFunc(func(*http.Request) (*http.Response, error) {
				return languageServerExpansionResponse(http.StatusOK, []byte(test.body)), nil
			})}
			if _, err := latestTerraformLanguageServerRelease(context.Background(), "linux", "amd64"); err == nil {
				t.Fatalf("malformed checkpoint response %q was accepted", test.body)
			}
		})
	}
}

func languageServerExpansionClangdRelease() languageServerRelease {
	release := languageServerRelease{Tag: "19.1.2"}
	for _, platform := range []string{"linux", "mac", "windows"} {
		name := "clangd-" + platform + "-19.1.2.zip"
		release.Assets = append(release.Assets, languageServerReleaseAsset{
			Name: name, URL: "https://downloads.example/" + name,
		})
	}
	return release
}

func languageServerExpansionInterceptClangdRelease(t *testing.T) *int {
	t.Helper()
	program := "clangd/bin/" + executableName("clangd")
	archive := languageServerZip(t, map[string]string{program: "managed clangd"}, 0o755)
	releaseData, err := json.Marshal(languageServerExpansionClangdRelease())
	if err != nil {
		t.Fatal(err)
	}
	originalClient := languageServerReleaseHTTPClient
	t.Cleanup(func() { languageServerReleaseHTTPClient = originalClient })
	requests := 0
	languageServerReleaseHTTPClient = &http.Client{Transport: languageServerExpansionRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.Method != http.MethodGet {
			t.Errorf("release request method = %q", request.Method)
		}
		if request.URL.String() == "https://api.github.com/repos/clangd/clangd/releases/latest" {
			return languageServerExpansionResponse(http.StatusOK, releaseData), nil
		}
		for _, asset := range languageServerExpansionClangdRelease().Assets {
			if request.URL.String() == asset.URL {
				return languageServerExpansionResponse(http.StatusOK, archive), nil
			}
		}
		return nil, fmt.Errorf("unexpected network request: %s", request.URL)
	})}
	return &requests
}

func TestLanguageServerExpansionInstallCatalogReleaseExtractsExecutable(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("official standalone clangd archives are x86-64 only")
	}
	requests := languageServerExpansionInterceptClangdRelease(t)
	profile, ok := languageServerProfileByID("clangd")
	if !ok {
		t.Fatal("clangd profile is missing")
	}
	toolsDir := t.TempDir()
	installation, err := installCatalogLanguageServerRelease(context.Background(), toolsDir, profile)
	if err != nil {
		t.Fatal(err)
	}
	if *requests != 2 {
		t.Fatalf("release requests = %d, want latest release and archive only", *requests)
	}
	wantProgram := filepath.Join(toolsDir, "payload", "clangd", "bin", executableName("clangd"))
	if installation.Path != wantProgram || !reflect.DeepEqual(installation.Arguments, profile.Arguments) ||
		!reflect.DeepEqual(installation.ProbeArguments, profile.VersionArguments) {
		t.Fatalf("managed clangd installation = %#v, want program %q", installation, wantProgram)
	}
	if info, err := os.Stat(installation.Path); err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("managed clangd executable = %#v, %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(toolsDir, ".download.zip")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("downloaded archive was not removed: %v", err)
	}
}

func TestLanguageServerExpansionReleaseInstallReturnsOwnedManagedRoots(t *testing.T) {
	if runtime.GOARCH != "amd64" {
		t.Skip("official standalone clangd archives are x86-64 only")
	}
	requests := languageServerExpansionInterceptClangdRelease(t)
	profile, _ := languageServerProfileByID("clangd")
	dataDir := t.TempDir()
	installation, err := installLanguageServerTool(profile, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if *requests != 2 || !installation.ProbeRequired {
		t.Fatalf("release install requests = %d, ProbeRequired = %v", *requests, installation.ProbeRequired)
	}
	if installation.ManagedRoot != managedLanguageServerRoot(dataDir, "clangd") ||
		!validManagedLanguageServerStaging("clangd", installation.OwnedRoot) ||
		!pathWithin(installation.OwnedRoot, installation.Path) {
		t.Fatalf("release install roots = %#v", installation)
	}
}

func TestLanguageServerExpansionRubyPrerequisites(t *testing.T) {
	originalLookPath := languageServerLookPath
	originalRun := runLanguageServerInstallCommand
	t.Cleanup(func() {
		languageServerLookPath = originalLookPath
		runLanguageServerInstallCommand = originalRun
	})
	profile, _ := languageServerProfileByID("ruby")
	runLanguageServerInstallCommand = func(context.Context, string, []string, []string) ([]byte, error) {
		t.Fatal("an installer ran without its Ruby prerequisites")
		return nil, nil
	}

	languageServerLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	if _, err := installRubyLanguageServer(context.Background(), t.TempDir(), profile); err == nil || !strings.Contains(err.Error(), "Ruby is required") {
		t.Fatalf("missing Ruby error = %v", err)
	}

	languageServerLookPath = func(program string) (string, error) {
		if program == "ruby" {
			return "/tools/ruby", nil
		}
		return "", exec.ErrNotFound
	}
	if _, err := installRubyLanguageServer(context.Background(), t.TempDir(), profile); err == nil || !strings.Contains(err.Error(), "RubyGems is required") {
		t.Fatalf("missing RubyGems error = %v", err)
	}
}

func TestLanguageServerExpansionRubyInstallIsIsolatedAndWrapsLauncher(t *testing.T) {
	originalLookPath := languageServerLookPath
	originalRun := runLanguageServerInstallCommand
	t.Cleanup(func() {
		languageServerLookPath = originalLookPath
		runLanguageServerInstallCommand = originalRun
	})
	languageServerLookPath = func(program string) (string, error) {
		switch program {
		case "ruby":
			return "/tools/ruby", nil
		case "gem":
			return "/tools/gem", nil
		default:
			return "", exec.ErrNotFound
		}
	}
	calls := 0
	runLanguageServerInstallCommand = func(_ context.Context, program string, arguments, environment []string) ([]byte, error) {
		calls++
		if program != "/tools/gem" || len(environment) != 0 || len(arguments) != 7 ||
			!reflect.DeepEqual(arguments[:4], []string{"install", "ruby-lsp", "--no-document", "--install-dir"}) ||
			arguments[5] != "--bindir" {
			return nil, fmt.Errorf("unexpected gem invocation: %q %q, environment %q", program, arguments, environment)
		}
		gemHome, launcherDir := arguments[4], arguments[6]
		root := filepath.Dir(gemHome)
		if filepath.Base(gemHome) != "gems" || filepath.Base(launcherDir) != "launcher" || filepath.Dir(launcherDir) != root ||
			!validManagedLanguageServerStaging("ruby", root) {
			return nil, fmt.Errorf("gem paths are not isolated in Ruby staging: %q %q", gemHome, launcherDir)
		}
		if err := os.MkdirAll(launcherDir, 0o755); err != nil {
			return nil, err
		}
		launcher := filepath.Join(launcherDir, "ruby-lsp")
		if runtime.GOOS == "windows" {
			launcher += ".bat"
		}
		return nil, os.WriteFile(launcher, []byte("fake Ruby LSP"), 0o755)
	}
	profile, _ := languageServerProfileByID("ruby")
	dataDir := t.TempDir()
	installation, err := installLanguageServerTool(profile, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || installation.ManagedRoot != managedLanguageServerRoot(dataDir, "ruby") ||
		!validManagedLanguageServerStaging("ruby", installation.OwnedRoot) || !pathWithin(installation.OwnedRoot, installation.Path) ||
		!reflect.DeepEqual(installation.ProbeArguments, profile.VersionArguments) {
		t.Fatalf("Ruby installation = %#v after %d commands", installation, calls)
	}
	wrapper := filepath.Join(installation.OwnedRoot, "bin", "ruby-lsp")
	if runtime.GOOS == "windows" {
		wrapper += ".cmd"
	}
	if installation.Path != wrapper {
		t.Fatalf("Ruby wrapper path = %q, want %q", installation.Path, wrapper)
	}
	contents, err := os.ReadFile(wrapper)
	if err != nil || !strings.Contains(string(contents), "GEM_HOME") || !strings.Contains(string(contents), "launcher") {
		t.Fatalf("Ruby wrapper = %q, %v", contents, err)
	}
}

func TestLanguageServerExpansionCSharpPrerequisiteAndIsolatedInstall(t *testing.T) {
	originalLookPath := languageServerLookPath
	originalRun := runLanguageServerInstallCommand
	t.Cleanup(func() {
		languageServerLookPath = originalLookPath
		runLanguageServerInstallCommand = originalRun
	})
	profile, _ := languageServerProfileByID("csharp")
	runLanguageServerInstallCommand = func(context.Context, string, []string, []string) ([]byte, error) {
		t.Fatal("dotnet install ran without the .NET prerequisite")
		return nil, nil
	}
	languageServerLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	if _, err := installCSharpLanguageServer(context.Background(), t.TempDir(), profile); err == nil || !strings.Contains(err.Error(), ".NET SDK is required") {
		t.Fatalf("missing dotnet error = %v", err)
	}

	languageServerLookPath = func(program string) (string, error) {
		if program == "dotnet" {
			return "/tools/dotnet", nil
		}
		return "", exec.ErrNotFound
	}
	calls := 0
	runLanguageServerInstallCommand = func(_ context.Context, program string, arguments, environment []string) ([]byte, error) {
		calls++
		if program != "/tools/dotnet" || len(environment) != 0 || len(arguments) != 5 ||
			!reflect.DeepEqual(arguments[:4], []string{"tool", "install", "csharp-ls", "--tool-path"}) {
			return nil, fmt.Errorf("unexpected dotnet invocation: %q %q, environment %q", program, arguments, environment)
		}
		binDir := arguments[4]
		if filepath.Base(binDir) != "bin" || !validManagedLanguageServerStaging("csharp", filepath.Dir(binDir)) {
			return nil, fmt.Errorf("dotnet tool path is not isolated: %q", binDir)
		}
		if err := os.MkdirAll(binDir, 0o755); err != nil {
			return nil, err
		}
		return nil, os.WriteFile(filepath.Join(binDir, executableName("csharp-ls")), []byte("fake C# language server"), 0o755)
	}
	dataDir := t.TempDir()
	installation, err := installLanguageServerTool(profile, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || installation.Path != filepath.Join(installation.OwnedRoot, "bin", executableName("csharp-ls")) ||
		installation.ManagedRoot != managedLanguageServerRoot(dataDir, "csharp") ||
		!validManagedLanguageServerStaging("csharp", installation.OwnedRoot) || !installation.ProbeRequired {
		t.Fatalf("C# installation = %#v after %d commands", installation, calls)
	}
}

func languageServerExpansionFakeNode(t *testing.T, version string) string {
	t.Helper()
	directory := t.TempDir()
	name := "node"
	contents := "#!/bin/sh\nprintf '%s\\n' '" + version + "'\n"
	if runtime.GOOS == "windows" {
		name += ".cmd"
		contents = "@echo off\r\necho " + version + "\r\n"
	}
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLanguageServerExpansionPrismaRequiresNodeTwenty(t *testing.T) {
	originalLookPath := languageServerLookPath
	originalRun := runLanguageServerInstallCommand
	t.Cleanup(func() {
		languageServerLookPath = originalLookPath
		runLanguageServerInstallCommand = originalRun
	})
	profile, _ := languageServerProfileByID("prisma")
	for _, test := range []struct {
		name, version string
		accept        bool
	}{
		{name: "Node 19 rejected", version: "v19.9.0"},
		{name: "Node 20 accepted", version: "v20.11.1", accept: true},
		{name: "Node 22 accepted", version: "v22.1.0", accept: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			node := languageServerExpansionFakeNode(t, test.version)
			languageServerLookPath = func(program string) (string, error) {
				switch program {
				case "npm":
					return "/tools/npm", nil
				case "node", node:
					return node, nil
				default:
					return "", exec.ErrNotFound
				}
			}
			calls := 0
			runLanguageServerInstallCommand = func(_ context.Context, program string, arguments, environment []string) ([]byte, error) {
				calls++
				if program != "/tools/npm" || len(environment) != 0 || len(arguments) != 6 ||
					!reflect.DeepEqual(arguments[:4], []string{"install", "--no-audit", "--no-fund", "--prefix"}) ||
					arguments[5] != "@prisma/language-server@latest" {
					return nil, fmt.Errorf("unexpected Prisma npm invocation: %q %q, environment %q", program, arguments, environment)
				}
				prefix := arguments[4]
				if !validManagedLanguageServerStaging("prisma", filepath.Dir(prefix)) || filepath.Base(prefix) != "node" {
					return nil, fmt.Errorf("Prisma npm prefix is not isolated: %q", prefix)
				}
				packageDir := filepath.Join(prefix, "node_modules", "@prisma", "language-server")
				if err := os.MkdirAll(filepath.Join(packageDir, "bin"), 0o755); err != nil {
					return nil, err
				}
				manifest := `{"bin":{"prisma-language-server":"bin/server.js"}}`
				if err := os.WriteFile(filepath.Join(packageDir, "package.json"), []byte(manifest), 0o644); err != nil {
					return nil, err
				}
				return nil, os.WriteFile(filepath.Join(packageDir, "bin", "server.js"), []byte("// fake Prisma language server"), 0o644)
			}
			dataDir := t.TempDir()
			installation, err := installLanguageServerTool(profile, dataDir)
			if !test.accept {
				if err == nil || !strings.Contains(err.Error(), "Node.js 20 or newer is required") || calls != 0 {
					t.Fatalf("Node %s installation = %#v, %v after %d npm commands", test.version, installation, err, calls)
				}
				return
			}
			if err != nil {
				t.Fatalf("Node %s was rejected: %v", test.version, err)
			}
			if calls != 1 || installation.Path != node || len(installation.Arguments) != 2 || installation.Arguments[1] != "--stdio" ||
				!strings.HasSuffix(filepath.ToSlash(installation.Arguments[0]), "/@prisma/language-server/bin/server.js") ||
				installation.ManagedRoot != managedLanguageServerRoot(dataDir, "prisma") ||
				!validManagedLanguageServerStaging("prisma", installation.OwnedRoot) || !pathWithin(installation.OwnedRoot, installation.Arguments[0]) {
				t.Fatalf("Prisma installation with Node %s = %#v after %d npm commands", test.version, installation, calls)
			}
		})
	}
}
