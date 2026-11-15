package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func installCatalogLanguageServerRelease(ctx context.Context, toolsDir string, profile languageServerProfile) (languageServerInstallation, error) {
	var release languageServerRelease
	if profile.ID == "terraform" {
		var err error
		release, err = latestTerraformLanguageServerRelease(ctx, runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return languageServerInstallation{}, err
		}
	} else {
		repositories := map[string]string{
			"clangd": "clangd/clangd", "lua": "LuaLS/lua-language-server", "zls": "zigtools/zls",
			"tombi": "tombi-toml/tombi", "neocmake": "neocmakelsp/neocmakelsp",
		}
		var err error
		release, err = latestLanguageServerRelease(ctx, repositories[profile.ID])
		if err != nil {
			return languageServerInstallation{}, err
		}
	}
	if profile.ID == "zls" {
		if err := validateZLSLanguageServerRuntime(release.Tag); err != nil {
			return languageServerInstallation{}, err
		}
	}
	asset, err := languageServerCatalogReleaseAsset(profile.ID, release, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return languageServerInstallation{}, err
	}
	archive := filepath.Join(toolsDir, ".download"+languageServerArchiveExtension(asset.Name))
	if err := downloadLanguageServerRelease(ctx, asset.URL, archive); err != nil {
		return languageServerInstallation{}, err
	}
	defer os.Remove(archive)
	payload := filepath.Join(toolsDir, "payload")
	if err := os.Mkdir(payload, 0o755); err != nil {
		return languageServerInstallation{}, err
	}
	if err := extractLanguageServerArchive(archive, asset.Name, payload); err != nil {
		return languageServerInstallation{}, err
	}
	program, err := languageServerFindReleaseExecutable(payload, executableName(profile.Program))
	if err != nil {
		return languageServerInstallation{}, err
	}
	if err := os.Chmod(program, 0o755); err != nil {
		return languageServerInstallation{}, err
	}
	return languageServerInstallation{
		Path: program, Arguments: append([]string(nil), profile.Arguments...),
		ProbeArguments: append([]string(nil), profile.VersionArguments...),
	}, nil
}

func validateZLSLanguageServerRuntime(zlsVersion string) error {
	zig, err := languageServerLookPath("zig")
	if err != nil {
		return errors.New("a compatible Zig toolchain is required before installing ZLS")
	}
	output, err := runLanguageServerVersionArguments(zig, nil, []string{"version"})
	if err != nil {
		return fmt.Errorf("verify the Zig toolchain required by ZLS: %w", err)
	}
	majorMinor := func(version string) string {
		parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(version), "v"), ".")
		if len(parts) < 2 {
			return ""
		}
		return parts[0] + "." + parts[1]
	}
	zigRelease, zlsRelease := majorMinor(output), majorMinor(zlsVersion)
	if zigRelease == "" || zigRelease != zlsRelease {
		return fmt.Errorf("Zig %s is not compatible with ZLS %s; install the matching Zig toolchain", strings.TrimSpace(output), strings.TrimPrefix(zlsVersion, "v"))
	}
	return nil
}

func languageServerCatalogReleaseAsset(id string, release languageServerRelease, goos, goarch string) (languageServerReleaseAsset, error) {
	version := strings.TrimPrefix(release.Tag, "v")
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[goarch]
	assetName := ""
	switch id {
	case "clangd":
		if goarch != "amd64" {
			return languageServerReleaseAsset{}, fmt.Errorf("clangd has no official %s/%s release archive", goos, goarch)
		}
		if goos != "linux" && goos != "darwin" && goos != "windows" {
			return languageServerReleaseAsset{}, fmt.Errorf("clangd managed installation is unavailable on %s", goos)
		}
		platform := map[string]string{"linux": "linux", "darwin": "mac", "windows": "windows"}[goos]
		assetName = "clangd-" + platform + "-" + release.Tag + ".zip"
	case "lua":
		architecture := map[string]string{"amd64": "x64", "arm64": "arm64"}[goarch]
		if architecture == "" || goos == "windows" && goarch != "amd64" {
			return languageServerReleaseAsset{}, fmt.Errorf("Lua Language Server has no official %s/%s release archive", goos, goarch)
		}
		platform := map[string]string{"linux": "linux", "darwin": "darwin", "windows": "win32"}[goos]
		if platform == "" {
			return languageServerReleaseAsset{}, fmt.Errorf("Lua Language Server managed installation is unavailable on %s", goos)
		}
		extension := ".tar.gz"
		if goos == "windows" {
			extension = ".zip"
		}
		assetName = "lua-language-server-" + release.Tag + "-" + platform + "-" + architecture + extension
	case "zls":
		architecture := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[goarch]
		platform := map[string]string{"linux": "linux", "darwin": "macos", "windows": "windows"}[goos]
		if architecture == "" || platform == "" {
			return languageServerReleaseAsset{}, fmt.Errorf("ZLS has no official %s/%s release archive", goos, goarch)
		}
		extension := ".tar.xz"
		if goos == "windows" {
			extension = ".zip"
		}
		assetName = "zls-" + architecture + "-" + platform + extension
	case "tombi":
		if arch == "" {
			return languageServerReleaseAsset{}, fmt.Errorf("Tombi has no official %s/%s release archive", goos, goarch)
		}
		platform := map[string]string{"linux": arch + "-unknown-linux-musl", "darwin": arch + "-apple-darwin", "windows": arch + "-pc-windows-msvc"}[goos]
		if platform == "" {
			return languageServerReleaseAsset{}, fmt.Errorf("Tombi managed installation is unavailable on %s", goos)
		}
		extension := ".tar.gz"
		if goos == "windows" {
			extension = ".zip"
		}
		assetName = "tombi-cli-" + version + "-" + platform + extension
	case "neocmake":
		platform := ""
		if goos == "darwin" && (goarch == "amd64" || goarch == "arm64") {
			platform = "universal-apple-darwin"
		} else if arch != "" {
			platform = map[string]string{"linux": arch + "-unknown-linux-musl", "windows": arch + "-pc-windows-msvc"}[goos]
		}
		if platform == "" {
			return languageServerReleaseAsset{}, fmt.Errorf("NeoCMakeLSP has no official %s/%s release archive", goos, goarch)
		}
		extension := ".tar.gz"
		if goos == "windows" {
			extension = ".zip"
		}
		assetName = "neocmakelsp-" + platform + extension
	case "terraform":
		if goos != "linux" && goos != "darwin" && goos != "windows" {
			return languageServerReleaseAsset{}, fmt.Errorf("Terraform Language Server managed installation is unavailable on %s", goos)
		}
		if arch == "" {
			return languageServerReleaseAsset{}, fmt.Errorf("Terraform Language Server has no official %s/%s release archive", goos, goarch)
		}
		terraformArch := map[string]string{"amd64": "amd64", "arm64": "arm64"}[goarch]
		assetName = "terraform-ls_" + version + "_" + goos + "_" + terraformArch + ".zip"
	default:
		return languageServerReleaseAsset{}, fmt.Errorf("no managed release installer for %s", id)
	}
	return languageServerAsset(release, assetName)
}

func latestTerraformLanguageServerRelease(ctx context.Context, goos, goarch string) (languageServerRelease, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://checkpoint-api.hashicorp.com/v1/check/terraform-ls", nil)
	if err != nil {
		return languageServerRelease{}, err
	}
	request.Header.Set("User-Agent", "TanGit language-server installer")
	response, err := languageServerReleaseHTTPClient.Do(request)
	if err != nil {
		return languageServerRelease{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return languageServerRelease{}, fmt.Errorf("find Terraform Language Server release: %s", response.Status)
	}
	var checkpoint struct {
		CurrentVersion string `json:"current_version"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&checkpoint); err != nil {
		return languageServerRelease{}, fmt.Errorf("read Terraform Language Server release: %w", err)
	}
	if !languageServerSemanticVersion(checkpoint.CurrentVersion) {
		return languageServerRelease{}, errors.New("Terraform release service returned an invalid version")
	}
	version := checkpoint.CurrentVersion
	name := "terraform-ls_" + version + "_" + goos + "_" + goarch + ".zip"
	return languageServerRelease{Tag: version, Assets: []languageServerReleaseAsset{{
		Name: name, URL: "https://releases.hashicorp.com/terraform-ls/" + version + "/" + name,
	}}}, nil
}

func languageServerSemanticVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		value, err := strconv.Atoi(part)
		if err != nil || value < 0 || strconv.Itoa(value) != part {
			return false
		}
	}
	return true
}

func installRubyLanguageServer(ctx context.Context, toolsDir string, profile languageServerProfile) (languageServerInstallation, error) {
	if _, err := languageServerLookPath("ruby"); err != nil {
		return languageServerInstallation{}, errors.New("Ruby is required to run Ruby LSP")
	}
	gem, err := languageServerLookPath("gem")
	if err != nil {
		return languageServerInstallation{}, errors.New("RubyGems is required to download Ruby LSP")
	}
	gemHome := filepath.Join(toolsDir, "gems")
	launcherDir := filepath.Join(toolsDir, "launcher")
	if err := runLanguageServerInstaller(ctx, gem, []string{"install", "ruby-lsp", "--no-document", "--install-dir", gemHome, "--bindir", launcherDir}, nil); err != nil {
		return languageServerInstallation{}, err
	}
	installed := filepath.Join(launcherDir, "ruby-lsp")
	if runtime.GOOS == "windows" {
		installed += ".bat"
	}
	if info, err := os.Stat(installed); err != nil || !info.Mode().IsRegular() {
		if err == nil {
			err = errors.New("installed Ruby LSP launcher is not a regular file")
		}
		return languageServerInstallation{}, fmt.Errorf("find installed Ruby LSP launcher: %w", err)
	}
	binDir := filepath.Join(toolsDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return languageServerInstallation{}, err
	}
	if runtime.GOOS == "windows" {
		wrapper := filepath.Join(binDir, "ruby-lsp.cmd")
		contents := "@echo off\r\nset \"GEM_HOME=%~dp0..\\gems\"\r\nset \"GEM_PATH=%GEM_HOME%\"\r\ncall \"%~dp0..\\launcher\\ruby-lsp.bat\" %*\r\n"
		if err := os.WriteFile(wrapper, []byte(contents), 0o755); err != nil {
			return languageServerInstallation{}, err
		}
		return languageServerInstallation{Path: wrapper, ProbeArguments: profile.VersionArguments}, nil
	}
	wrapper := filepath.Join(binDir, "ruby-lsp")
	contents := "#!/bin/sh\nGEM_HOME=$(CDPATH= cd -- \"$(dirname -- \"$0\")/../gems\" && pwd) || exit 1\nGEM_PATH=$GEM_HOME\nexport GEM_HOME GEM_PATH\nexec \"$GEM_HOME/../launcher/ruby-lsp\" \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(contents), 0o755); err != nil {
		return languageServerInstallation{}, err
	}
	return languageServerInstallation{Path: wrapper, ProbeArguments: profile.VersionArguments}, nil
}

func installCSharpLanguageServer(ctx context.Context, toolsDir string, profile languageServerProfile) (languageServerInstallation, error) {
	dotnet, err := languageServerLookPath("dotnet")
	if err != nil {
		return languageServerInstallation{}, errors.New("the .NET SDK is required to install and run C# Language Server")
	}
	binDir := filepath.Join(toolsDir, "bin")
	if err := runLanguageServerInstaller(ctx, dotnet, []string{"tool", "install", "csharp-ls", "--tool-path", binDir}, nil); err != nil {
		return languageServerInstallation{}, err
	}
	program := filepath.Join(binDir, executableName("csharp-ls"))
	if info, err := os.Stat(program); err != nil || !info.Mode().IsRegular() {
		if err == nil {
			err = errors.New("installed C# language server is not a regular file")
		}
		return languageServerInstallation{}, fmt.Errorf("find installed C# language server: %w", err)
	}
	return languageServerInstallation{Path: program, ProbeArguments: profile.VersionArguments}, nil
}
