package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExpandedReleaseLanguageServersRealProtocolLifecycle(t *testing.T) {
	if os.Getenv("LAZYGITGO_TEST_REAL_LSP_INSTALL") != "1" {
		t.Skip("set LAZYGITGO_TEST_REAL_LSP_INSTALL=1 to download and exercise release language servers")
	}
	tests := []struct {
		id, file, language, text                                             string
		hoverMarker, completionMarker, completionWant                        string
		definitionMarker, definitionFile, replaceOld, replaceNew, diagnostic string
		definitionLine                                                       int
		files                                                                map[string]string
	}{
		{
			id: "lua", file: "main.lua", language: "lua",
			text:        "local lazygitgo_answer = 42\nlocal answer = lazygitgo_answer\nlazy\n",
			hoverMarker: "lazygitgo_answer\nlazy", completionMarker: "lazy\n", completionWant: "lazygitgo_answer",
			definitionMarker: "lazygitgo_answer\nlazy", definitionLine: 0,
			replaceOld: "lazy\n", replaceNew: "missing_name\n", diagnostic: "missing_name",
		},
		{
			id: "neocmake", file: "CMakeLists.txt", language: "cmake",
			text:        "cmake_minimum_required(VERSION 3.20)\nproject(lazygitGo)\nfunction(lazygitgo_answer)\n  set(LAZYGITGO_ANSWER 42)\nendfunction()\nlazygitgo_answer()\ninclude(lazygitGoHelpers.cmake)\nlazygitgo_ans\n",
			hoverMarker: "lazygitgo_answer()\ninclude", completionMarker: "lazygitgo_ans\n", completionWant: "lazygitgo_answer",
			definitionMarker: "lazygitGoHelpers.cmake)\nlazygitgo_ans", definitionFile: "lazygitGoHelpers.cmake", definitionLine: 0,
			replaceOld: "lazygitgo_ans\n", replaceNew: "PROJECT(lazygitGo)\n", diagnostic: "case",
			files: map[string]string{"lazygitGoHelpers.cmake": "set(LAZYGITGO_ANSWER 42)\n", ".neocmake.toml": "command_case = \"lower_case\"\n"},
		},
		{
			id: "terraform", file: "main.tf", language: "terraform",
			text:        "variable \"lazygitgo_answer\" {\n  type    = number\n  default = 42\n}\n\noutput \"answer\" {\n  value = var.lazygitgo_answer\n}\n\noutput \"completion\" {\n  value = var.lazy\n}\n",
			hoverMarker: "var.lazygitgo_answer", completionMarker: "var.lazy\n", completionWant: "lazygitgo_answer",
			definitionMarker: "var.lazygitgo_answer", definitionLine: 0,
			replaceOld: "var.lazy\n", replaceNew: "var.missing_name\n", diagnostic: "missing_name",
		},
		{
			id: "tombi", file: "pyproject.toml", language: "toml",
			text:        "#:schema ./schema.json\n\n[project]\nname = \"lazygitgo\"\ndesc\n",
			hoverMarker: "name = \"lazygitgo\"", completionMarker: "desc\n", completionWant: "description",
			definitionMarker: "./schema.json", definitionFile: "schema.json", definitionLine: 0,
			replaceOld: "name = \"lazygitgo\"\ndesc\n", replaceNew: "name = 42\ndescription = \"ok\"\n", diagnostic: "string",
			files: map[string]string{"schema.json": `{
  "type": "object",
  "properties": {
    "project": {
      "type": "object",
      "properties": {
        "name": {"type": "string", "description": "lazygitGo project name"},
        "description": {"type": "string", "description": "lazygitGo project description"}
      }
    }
  }
}
`},
		},
		{
			id: "zls", file: "main.zig", language: "zig",
			text:        "const lazygitgo_answer: i32 = 42;\npub fn main() void {\n    const answer = lazygitgo_answer;\n    _ = answer;\n    _ = lazy;\n}\n",
			hoverMarker: "lazygitgo_answer;\n    _ = answer", completionMarker: "_ = lazy;", completionWant: "lazygitgo_answer",
			definitionMarker: "lazygitgo_answer;\n    _ = answer", definitionLine: 0,
			replaceOld: "_ = lazy;", replaceNew: "_ = missing_name;", diagnostic: "missing_name",
		},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			if test.id == "zls" {
				if _, err := languageServerLookPath("zig"); err != nil {
					t.Skip("a compatible Zig toolchain is not installed")
				}
			}
			profile := mustLanguageServerProfile(t, test.id)
			if test.id != "terraform" {
				useOfficialLanguageServerReleaseSnapshot(t, profile.ID)
			}
			installation, dataDir := installRealExpandedLanguageServer(t, profile)
			workspace := t.TempDir()
			for file, contents := range test.files {
				path := filepath.Join(workspace, file)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			name := filepath.Join(workspace, test.file)
			if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(name, []byte(test.text), 0o644); err != nil {
				t.Fatal(err)
			}
			server, diagnostics := openRealExpandedLanguageServer(t, installation, dataDir, workspace, name, test.id, test.language, test.text)
			defer server.Close()
			uri := languageServerFileURI(name)
			waitForRealExpandedLanguageServerReady(t, diagnostics, test.id)
			// Incomplete input makes schema-aware servers publish their first
			// diagnostics. Waiting for that event proves didOpen was analyzed
			// before issuing feature requests instead of racing their parser.
			// NeoCMakeLSP only reports lint violations, not incomplete commands.
			if test.id != "neocmake" {
				waitForRealExpandedDiagnostic(t, diagnostics, uri, "")
			}
			offset := func(marker string) int {
				index := strings.Index(test.text, marker)
				if index < 0 {
					t.Fatalf("test marker %q is absent", marker)
				}
				return index
			}
			hover := requestRealExpandedLanguageServer(t, server, "textDocument/hover", realExpandedLanguageServerPosition(uri, test.text, offset(test.hoverMarker)))
			if content, _, ok := parseLanguageServerHover(hover); !ok || strings.TrimSpace(content) == "" {
				t.Fatalf("%s hover = %s", test.id, hover)
			}
			completionOffset := offset(test.completionMarker) + len(test.completionMarker) - 1
			completions := requestRealExpandedLanguageServer(t, server, "textDocument/completion", realExpandedLanguageServerPosition(uri, test.text, completionOffset))
			assertRealExpandedCompletion(t, completions, test.completionWant)
			if test.definitionLine >= 0 {
				definition := requestRealExpandedLanguageServer(t, server, "textDocument/definition", realExpandedLanguageServerPosition(uri, test.text, offset(test.definitionMarker)))
				location, ok := firstLanguageServerLocation(definition)
				expectedURI := uri
				if test.definitionFile != "" {
					expectedURI = languageServerFileURI(filepath.Join(workspace, test.definitionFile))
				}
				if !ok || location.URI != expectedURI || location.Range.Start.Line != test.definitionLine {
					t.Fatalf("%s definition = %s, want %s:%d", test.id, definition, expectedURI, test.definitionLine)
				}
			}
			changed := strings.Replace(test.text, test.replaceOld, test.replaceNew, 1)
			server.DidChange(uri, changed, 2)
			if test.id == "neocmake" {
				report := requestRealExpandedLanguageServer(t, server, "textDocument/diagnostic", languageServerDiagnosticParams{TextDocument: languageServerTextDocument{URI: uri}})
				if !strings.Contains(strings.ToLower(string(report)), strings.ToLower(test.diagnostic)) {
					t.Fatalf("NeoCMakeLSP diagnostic report does not contain %q: %s", test.diagnostic, report)
				}
			} else {
				waitForRealExpandedDiagnostic(t, diagnostics, uri, test.diagnostic)
			}
		})
	}
}

// useOfficialLanguageServerReleaseSnapshot keeps real protocol tests independent
// of GitHub's anonymous latest-release API quota. Only the discovery response is
// a fixed official-release snapshot; the installer still downloads and extracts
// the actual bytes from the upstream HTTPS release URL.
func useOfficialLanguageServerReleaseSnapshot(t *testing.T, id string) {
	t.Helper()
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("no release snapshot for this architecture")
	}
	var repository, tag, name string
	arch := map[string]string{"amd64": "x86_64", "arm64": "aarch64"}[runtime.GOARCH]
	switch id {
	case "lua":
		repository, tag = "LuaLS/lua-language-server", "3.19.2"
		platform := map[string]string{"linux": "linux", "darwin": "darwin", "windows": "win32"}[runtime.GOOS]
		architecture := map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
		extension := ".tar.gz"
		if runtime.GOOS == "windows" {
			extension = ".zip"
		}
		name = fmt.Sprintf("lua-language-server-%s-%s-%s%s", tag, platform, architecture, extension)
	case "tombi":
		repository, tag = "tombi-toml/tombi", "v1.11.2"
		platform := map[string]string{"linux": "-unknown-linux-musl", "darwin": "-apple-darwin", "windows": "-pc-windows-msvc"}[runtime.GOOS]
		extension := ".tar.gz"
		if runtime.GOOS == "windows" {
			extension = ".zip"
		}
		name = fmt.Sprintf("tombi-cli-%s-%s%s%s", strings.TrimPrefix(tag, "v"), arch, platform, extension)
	case "neocmake":
		repository, tag = "neocmakelsp/neocmakelsp", "v0.12.0"
		platform := arch + map[string]string{"linux": "-unknown-linux-musl", "windows": "-pc-windows-msvc"}[runtime.GOOS]
		if runtime.GOOS == "darwin" {
			platform = "universal-apple-darwin"
		}
		extension := ".tar.gz"
		if runtime.GOOS == "windows" {
			extension = ".zip"
		}
		name = "neocmakelsp-" + platform + extension
	case "zls":
		repository, tag = "zigtools/zls", "0.17.0"
		platform := map[string]string{"linux": "linux", "darwin": "macos", "windows": "windows"}[runtime.GOOS]
		extension := ".tar.xz"
		if runtime.GOOS == "windows" {
			extension = ".zip"
		}
		name = fmt.Sprintf("zls-%s-%s%s", arch, platform, extension)
	default:
		t.Fatalf("no official release snapshot for %s", id)
	}
	if repository == "" || strings.HasSuffix(name, "-.") || strings.Contains(name, "--") {
		t.Skipf("no official %s/%s release snapshot for %s", runtime.GOOS, runtime.GOARCH, id)
	}
	release := languageServerRelease{Tag: tag, Assets: []languageServerReleaseAsset{{
		Name: name,
		URL:  fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", repository, tag, name),
	}}}
	body, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	original := languageServerReleaseHTTPClient
	t.Cleanup(func() { languageServerReleaseHTTPClient = original })
	languageServerReleaseHTTPClient = &http.Client{Transport: languageServerExpansionRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Host == "api.github.com" {
			return languageServerExpansionResponse(http.StatusOK, body), nil
		}
		return http.DefaultClient.Do(request)
	})}
}
