package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRubyAndCSharpRealProtocolLifecycle(t *testing.T) {
	if os.Getenv("LAZYGITGO_TEST_REAL_LSP_INSTALL") != "1" {
		t.Skip("set LAZYGITGO_TEST_REAL_LSP_INSTALL=1 to install and exercise toolchain language servers")
	}
	tests := []struct {
		id, runtime, file, language, text                    string
		hoverMarker, completionMarker, completionWant        string
		definitionMarker, replaceOld, replaceNew, diagnostic string
		files                                                map[string]string
		definitionLine                                       int
		waitInitially                                        bool
	}{
		{
			id: "ruby", runtime: "ruby", file: "main.rb", language: "ruby",
			text:        "class LazygitGoAnswer\n  def value\n    42\n  end\nend\nLazygitGoAnswer.new.value\nLazygitGoAns\n",
			hoverMarker: "LazygitGoAnswer.new.value", completionMarker: "LazygitGoAns\n", completionWant: "LazygitGoAnswer",
			definitionMarker: "LazygitGoAnswer.new.value", replaceOld: "LazygitGoAns\n", replaceNew: "def broken(\n", diagnostic: "expected",
		},
		{
			id: "csharp", runtime: "dotnet", file: "Program.cs", language: "csharp",
			text:        "namespace lazygitGo;\n\npublic static class Program\n{\n    public static void Main()\n    {\n        var lazygitgo_answer = 42;\n        System.Console.WriteLine(lazygitgo_answer);\n        System.Console.WriteLine(lazygitgo_ans);\n    }\n}\n",
			hoverMarker: "lazygitgo_answer);", completionMarker: "lazygitgo_ans);", completionWant: "lazygitgo_answer",
			definitionMarker: "lazygitgo_answer);", replaceOld: "lazygitgo_ans);", replaceNew: "missing_name);", diagnostic: "missing_name",
			files:          map[string]string{"lazygitGo.csproj": "<Project Sdk=\"Microsoft.NET.Sdk\"><PropertyGroup><OutputType>Exe</OutputType><TargetFramework>net10.0</TargetFramework></PropertyGroup></Project>\n"},
			definitionLine: 6, waitInitially: true,
		},
	}
	for _, test := range tests {
		t.Run(test.id, func(t *testing.T) {
			if _, err := languageServerLookPath(test.runtime); err != nil {
				t.Skipf("the external %s toolchain is not installed", test.runtime)
			}
			profile := mustLanguageServerProfile(t, test.id)
			installation, dataDir := installRealExpandedLanguageServer(t, profile)
			workspace := t.TempDir()
			for file, contents := range test.files {
				if err := os.WriteFile(filepath.Join(workspace, file), []byte(contents), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			name := filepath.Join(workspace, test.file)
			if err := os.WriteFile(name, []byte(test.text), 0o644); err != nil {
				t.Fatal(err)
			}
			server, diagnostics := openRealExpandedLanguageServer(t, installation, dataDir, workspace, name, test.id, test.language, test.text)
			defer server.Close()
			uri := languageServerFileURI(name)
			if test.waitInitially {
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
			definition := requestRealExpandedLanguageServer(t, server, "textDocument/definition", realExpandedLanguageServerPosition(uri, test.text, offset(test.definitionMarker)))
			location, ok := firstLanguageServerLocation(definition)
			if !ok || location.URI != uri || location.Range.Start.Line != test.definitionLine {
				t.Fatalf("%s definition = %s, want %s:%d", test.id, definition, uri, test.definitionLine)
			}
			changed := strings.Replace(test.text, test.replaceOld, test.replaceNew, 1)
			server.DidChange(uri, changed, 2)
			waitForRealExpandedDiagnostic(t, diagnostics, uri, test.diagnostic)
		})
	}
}
