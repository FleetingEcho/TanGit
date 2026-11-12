package app

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// These strings deliberately reach tr unchanged. They are data, keyboard
// shortcuts, formatting, or examples rather than prose that needs a catalog
// entry. Keep this list exact: a missing translation must not be dismissed by
// a broad "technical looking" heuristic.
var i18nIdentitySourceKeys = map[string]string{
	"":                                 "empty optional message",
	"%s %d":                            "formatting only",
	"/path/to/repository":              "path example",
	"Ctrl+Enter":                       "keyboard shortcut",
	"F3":                               "keyboard shortcut",
	"F3 / Enter":                       "keyboard shortcuts",
	"F5":                               "keyboard shortcut",
	"HEAD":                             "Git ref",
	"Shift+F3":                         "keyboard shortcut",
	"feature or HEAD":                  "branch/ref example",
	"feature/my-branch":                "branch example",
	"feature/my-work":                  "branch example",
	"git@example.com:team/library.git": "repository URL example",
	"git@example.com:team/project.git": "repository URL example",
	"https://example.com/team/project": "repository URL example",
	"library":                          "repository name example",
	"main or HEAD~1":                   "branch/ref example",
	"origin":                           "conventional Git remote name",
	"v1.0.0":                           "version example",
	"v1.2.0":                           "version example",
	"vendor/library":                   "repository path example",
	"●":                                "status symbol",
}

// Raw UI sinks need their own, smaller review. These exact strings are useful
// verbatim as commands, syntax examples, product names, placeholders, or
// symbols. Dynamic expressions are not classified as translated by this gate.
var i18nIdentityRawUIStrings = map[string]string{
	"--version":                     "command-line example",
	"-remote=auto":                  "command-line example",
	".py=python, .pyi=python":       "file association syntax example",
	"/home/user/project":            "path placeholder",
	"Production":                    "example profile name",
	"VS Code":                       "product name",
	"Zed":                           "product name",
	"git":                           "command name",
	"Go":                            "programming language/product name",
	"gopls, /path/to/gopls":         "command/path placeholder",
	"HEAD":                          "Git ref",
	"https://pypi.org/project/ty/":  "download URL example",
	"my-editor --wait {{filename}}": "command template placeholder",
	"node_modules":                  "directory name placeholder",
	"ripgrep":                       "program name",
	"server":                        "command placeholder",
	"server.example.com":            "host placeholder",
	"stash@{%d} · %s":               "Git ref and metadata format",
	"ty":                            "command placeholder",
	"ty, /path/to/ty":               "command/path placeholder",
	"~/.ssh/id_ed25519":             "private-key path placeholder",
	"~/src/project":                 "path placeholder",
	"Aa":                            "case-sensitive search glyph",
	"ab":                            "whole-word search glyph",
	"C":                             "keyboard shortcut glyph",
	"Ctrl+,":                        "keyboard shortcut glyph",
	"tangit <path>":                 "command-line usage hint",
}

type i18nSourcePos struct {
	File string
	Line int
}

type i18nSourceRef struct {
	Pos    i18nSourcePos
	Origin string
}

type i18nSourceFile struct {
	name  string
	scope string
	file  *ast.File
}

type i18nSourceFunction struct {
	key          string
	receiver     string
	receiverName string
	decl         *ast.FuncDecl
	params       []string
	translated   map[int]bool
	rawUI        map[int]bool
}

type i18nSourceCall struct {
	owner  string
	callee string
	call   *ast.CallExpr
	file   string
}

type i18nSourceAnalysis struct {
	keys               map[string][]i18nSourceRef
	rawUI              map[string][]i18nSourceRef
	dynamicDirectCalls int
}

// TestTranslationSourceKeysAreCataloged derives required keys from source,
// then checks the actual runtime maps after all additive catalog init functions
// have run. It intentionally does not parse the catalog source into a second
// implementation of catalog assembly.
func TestTranslationSourceKeysAreCataloged(t *testing.T) {
	files, fset := loadI18nSourceFiles(t)
	analysis := analyzeI18nSources(files, fset)

	type missingKey struct {
		languages []string
		refs      []i18nSourceRef
	}
	missing := map[string]missingKey{}
	for key, refs := range analysis.keys {
		if _, identity := i18nIdentitySourceKeys[key]; identity {
			continue
		}
		var languages []string
		for _, choice := range languageChoices[1:] {
			if _, ok := translations[choice.Code][key]; !ok {
				languages = append(languages, choice.Code)
			}
		}
		if len(languages) > 0 {
			missing[key] = missingKey{languages: languages, refs: refs}
		}
	}

	t.Logf("checked %d distinct statically resolved translation keys; %d nonliteral direct tr/trf sites are outside this static gate and are not claimed as covered", len(analysis.keys), analysis.dynamicDirectCalls)
	if len(missing) == 0 {
		return
	}

	// One deterministic, source-grouped diagnostic makes every omission
	// actionable without hiding later failures behind t.Fatal.
	var diagnostics strings.Builder
	fmt.Fprintf(&diagnostics, "source/catalog coverage failed: %d distinct statically referenced keys are absent from runtime catalogs\n", len(missing))
	var keys []string
	for key := range missing {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var refs []struct {
		key       string
		languages string
		ref       i18nSourceRef
	}
	for _, key := range keys {
		item := missing[key]
		for _, ref := range item.refs {
			refs = append(refs, struct {
				key       string
				languages string
				ref       i18nSourceRef
			}{key, strings.Join(item.languages, ", "), ref})
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		a, b := refs[i], refs[j]
		if a.ref.Pos.File != b.ref.Pos.File {
			return a.ref.Pos.File < b.ref.Pos.File
		}
		if a.ref.Pos.Line != b.ref.Pos.Line {
			return a.ref.Pos.Line < b.ref.Pos.Line
		}
		if a.key != b.key {
			return a.key < b.key
		}
		return a.ref.Origin < b.ref.Origin
	})
	lastFile := ""
	for _, item := range refs {
		if item.ref.Pos.File != lastFile {
			lastFile = item.ref.Pos.File
			fmt.Fprintf(&diagnostics, "\n%s:\n", lastFile)
		}
		via := ""
		if item.ref.Origin != "" {
			via = " via " + item.ref.Origin
		}
		fmt.Fprintf(&diagnostics, "  %s:%d: %q%s; missing in %s\n", item.ref.Pos.File, item.ref.Pos.Line, item.key, via, item.languages)
	}
	t.Error(diagnostics.String())
}

// TestRawUIStringsAreTranslated is deliberately separate from catalog
// coverage: a string can have a catalog entry and still bypass tr at the UI
// sink. It checks direct constants and constants forwarded through local UI
// wrappers. Dynamic values and symbols are not asserted to be covered.
func TestRawUIStringsAreTranslated(t *testing.T) {
	files, fset := loadI18nSourceFiles(t)
	analysis := analyzeI18nSources(files, fset)

	missing := map[string][]i18nSourceRef{}
	for text, refs := range analysis.rawUI {
		if _, identity := i18nIdentityRawUIStrings[text]; identity {
			continue
		}
		missing[text] = refs
	}
	if len(missing) == 0 {
		return
	}

	var keys []string
	for key := range missing {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var refs []struct {
		key string
		ref i18nSourceRef
	}
	for _, key := range keys {
		for _, ref := range missing[key] {
			refs = append(refs, struct {
				key string
				ref i18nSourceRef
			}{key, ref})
		}
	}
	sort.Slice(refs, func(i, j int) bool {
		a, b := refs[i], refs[j]
		if a.ref.Pos.File != b.ref.Pos.File {
			return a.ref.Pos.File < b.ref.Pos.File
		}
		if a.ref.Pos.Line != b.ref.Pos.Line {
			return a.ref.Pos.Line < b.ref.Pos.Line
		}
		if a.key != b.key {
			return a.key < b.key
		}
		return a.ref.Origin < b.ref.Origin
	})
	var diagnostics strings.Builder
	fmt.Fprintf(&diagnostics, "raw UI coverage failed: %d distinct constant strings reach UI sinks without tr/trf\n", len(missing))
	lastFile := ""
	for _, item := range refs {
		if item.ref.Pos.File != lastFile {
			lastFile = item.ref.Pos.File
			fmt.Fprintf(&diagnostics, "\n%s:\n", lastFile)
		}
		fmt.Fprintf(&diagnostics, "  %s:%d: %q at %s\n", item.ref.Pos.File, item.ref.Pos.Line, item.key, item.ref.Origin)
	}
	t.Error(diagnostics.String())
}

// loadI18nSourceFiles audits every production Go file in the repository, not
// only the package that happens to host the test. Third-party source is not our
// UI and is deliberately excluded. Tests are excluded because their English
// assertions are not rendered to users.
func loadI18nSourceFiles(t testing.TB) ([]i18nSourceFile, *token.FileSet) {
	t.Helper()
	root := filepath.Clean(filepath.Join("..", ".."))
	fset := token.NewFileSet()
	var files []i18nSourceFile
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".agents", "third_party", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", relative, err)
		}
		files = append(files, i18nSourceFile{name: relative, scope: filepath.ToSlash(filepath.Dir(relative)), file: file})
		return nil
	})
	if err != nil {
		t.Fatalf("walk repository source: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no production source files found")
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	return files, fset
}

func TestI18nSourceAuditUsesFullCodebase(t *testing.T) {
	files, _ := loadI18nSourceFiles(t)
	found := map[string]bool{}
	for _, source := range files {
		found[source.name] = true
	}
	for _, required := range []string{
		"cmd/tangit/main.go",
		"internal/app/i18n.go",
		"internal/repository/session.go",
		"internal/sshworkspace/profile.go",
		"internal/transfer/manager.go",
	} {
		if !found[required] {
			t.Errorf("full-codebase i18n audit did not scan %s", required)
		}
	}
}

func analyzeI18nSources(files []i18nSourceFile, fset *token.FileSet) i18nSourceAnalysis {
	functions := map[string]*i18nSourceFunction{}
	for _, source := range files {
		for _, declaration := range source.file.Decls {
			decl, ok := declaration.(*ast.FuncDecl)
			if !ok || decl.Body == nil {
				continue
			}
			key := i18nScopedFunctionKey(source, decl)
			receiver, receiverName := i18nReceiverInfo(decl)
			functions[key] = newI18nSourceFunction(decl, key, receiver, receiverName)

			// Named function literals are common local UI helpers. Give them a
			// private key so they do not collide with package functions.
			addLiteral := func(id *ast.Ident, literal *ast.FuncLit) {
				fake := &ast.FuncDecl{Name: id, Type: literal.Type, Body: literal.Body}
				literalKey := key + "$" + id.Name
				functions[literalKey] = newI18nSourceFunction(fake, literalKey, "", "")
			}
			ast.Inspect(decl.Body, func(node ast.Node) bool {
				switch value := node.(type) {
				case *ast.AssignStmt:
					for i, lhs := range value.Lhs {
						id, ok := lhs.(*ast.Ident)
						if !ok || i >= len(value.Rhs) {
							continue
						}
						if literal, ok := value.Rhs[i].(*ast.FuncLit); ok {
							addLiteral(id, literal)
						}
					}
				case *ast.ValueSpec:
					for i, id := range value.Names {
						if i < len(value.Values) {
							if literal, ok := value.Values[i].(*ast.FuncLit); ok {
								addLiteral(id, literal)
							}
						}
					}
				}
				return true
			})
		}
	}

	var calls []i18nSourceCall
	locals := map[string]map[string]map[string]bool{}
	for _, source := range files {
		ast.Inspect(source.file, func(node ast.Node) bool {
			decl, ok := node.(*ast.FuncDecl)
			if !ok || decl.Body == nil {
				return true
			}
			owner := i18nScopedFunctionKey(source, decl)
			locals[owner] = i18nLocalConstants(decl.Body)
			ast.Inspect(decl.Body, func(child ast.Node) bool {
				call, ok := child.(*ast.CallExpr)
				if !ok {
					return true
				}
				callee := i18nScopedName(source.scope, i18nCalleeKey(call, functions[owner]))
				if id, ok := call.Fun.(*ast.Ident); ok && functions[owner+"$"+id.Name] != nil {
					callee = owner + "$" + id.Name
				}
				calls = append(calls, i18nSourceCall{owner: owner, callee: callee, call: call, file: source.name})
				return true
			})
			return false
		})
	}

	inferI18nParameterUses(functions)
	propagateI18nParameterUses(functions, calls)

	analysis := i18nSourceAnalysis{
		keys:  map[string][]i18nSourceRef{},
		rawUI: map[string][]i18nSourceRef{},
	}
	staticValues := func(owner string, expression ast.Expr) map[string]bool {
		if text, ok := i18nStaticString(expression); ok {
			return map[string]bool{text: true}
		}
		if id, ok := expression.(*ast.Ident); ok {
			return locals[owner][id.Name]
		}
		return nil
	}
	addUses := func(target map[string][]i18nSourceRef, values map[string]bool, call *ast.CallExpr, file, origin string) {
		pos := fset.Position(call.Pos())
		for text := range values {
			target[text] = append(target[text], i18nSourceRef{
				Pos:    i18nSourcePos{File: file, Line: pos.Line},
				Origin: origin,
			})
		}
	}

	// Direct tr/trf calls, including simple local constants.
	for _, source := range files {
		ast.Inspect(source.file, func(node ast.Node) bool {
			decl, ok := node.(*ast.FuncDecl)
			if !ok || decl.Body == nil {
				return true
			}
			owner := i18nScopedFunctionKey(source, decl)
			ast.Inspect(decl.Body, func(child ast.Node) bool {
				call, ok := child.(*ast.CallExpr)
				if !ok || !i18nIsTranslationCall(call) {
					return true
				}
				values := staticValues(owner, call.Args[0])
				if len(values) == 0 {
					analysis.dynamicDirectCalls++
					return true
				}
				addUses(analysis.keys, values, call, source.name, "")
				return true
			})
			return false
		})
	}

	// Arguments at call sites of functions whose corresponding parameters
	// reach tr/trf. The fixpoint above includes forwarding wrappers.
	for _, site := range calls {
		callee := functions[site.callee]
		if callee == nil || i18nCallName(site.call) == "tr" || i18nCallName(site.call) == "trf" {
			continue
		}
		for index := range callee.translated {
			if index < len(site.call.Args) {
				values := staticValues(site.owner, site.call.Args[index])
				addUses(analysis.keys, values, site.call, site.file, site.callee)
			}
		}
		for index := range callee.rawUI {
			if index >= len(site.call.Args) || i18nContainsTranslationCall(site.call.Args[index]) {
				continue
			}
			values := staticValues(site.owner, site.call.Args[index])
			addUses(analysis.rawUI, values, site.call, site.file, site.callee)
		}
	}

	// Direct raw UI constructors and chainable sink methods.
	for _, source := range files {
		ast.Inspect(source.file, func(node ast.Node) bool {
			decl, ok := node.(*ast.FuncDecl)
			if !ok || decl.Body == nil {
				return true
			}
			owner := i18nScopedFunctionKey(source, decl)
			ast.Inspect(decl.Body, func(child ast.Node) bool {
				call, ok := child.(*ast.CallExpr)
				if !ok {
					return true
				}
				for _, index := range i18nRawSinkArgIndexes(call) {
					if index >= len(call.Args) || i18nContainsTranslationCall(call.Args[index]) {
						continue
					}
					values := staticValues(owner, call.Args[index])
					for text := range values {
						if i18nHasUIProse(text) {
							addUses(analysis.rawUI, map[string]bool{text: true}, call, source.name, i18nCallName(call))
						}
					}
				}
				return true
			})
			return false
		})

		// Native dialog accessibility text and editor labels are struct fields,
		// not method calls, so include their known text-bearing composites.
		ast.Inspect(source.file, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			typeName := i18nCompositeTypeName(literal.Type)
			var fields map[string]bool
			switch typeName {
			case "mygo.OpenDialogOptions", "mygo.SaveDialogOptions":
				fields = map[string]bool{"Title": true, "ButtonLabel": true, "Message": true}
			case "mygo.FileFilter":
				fields = map[string]bool{"Name": true}
			case "editor.State":
				fields = map[string]bool{"Label": true}
			}
			for _, element := range literal.Elts {
				field, ok := element.(*ast.KeyValueExpr)
				if !ok || !fields[i18nCompositeFieldName(field.Key)] || i18nContainsTranslationCall(field.Value) {
					continue
				}
				text, ok := i18nStaticString(field.Value)
				if !ok || !i18nHasUIProse(text) {
					continue
				}
				pos := fset.Position(field.Value.Pos())
				analysis.rawUI[text] = append(analysis.rawUI[text], i18nSourceRef{
					Pos:    i18nSourcePos{File: source.name, Line: pos.Line},
					Origin: typeName + "." + i18nCompositeFieldName(field.Key),
				})
			}
			return true
		})
	}

	// Dynamic raw values are intentionally ignored above. Only raw helper
	// arguments with prose are actionable; command glyphs and formatting-only
	// tokens are not language.
	for text, refs := range analysis.rawUI {
		if !i18nHasUIProse(text) {
			delete(analysis.rawUI, text)
			continue
		}
		sort.Slice(refs, func(i, j int) bool {
			if refs[i].Pos.File != refs[j].Pos.File {
				return refs[i].Pos.File < refs[j].Pos.File
			}
			return refs[i].Pos.Line < refs[j].Pos.Line
		})
	}
	return analysis
}

func newI18nSourceFunction(decl *ast.FuncDecl, key, receiver, receiverName string) *i18nSourceFunction {
	function := &i18nSourceFunction{
		key:          key,
		receiver:     receiver,
		receiverName: receiverName,
		decl:         decl,
		translated:   map[int]bool{},
		rawUI:        map[int]bool{},
	}
	if decl.Type.Params != nil {
		for _, field := range decl.Type.Params.List {
			for _, name := range field.Names {
				function.params = append(function.params, name.Name)
			}
		}
	}
	return function
}

func inferI18nParameterUses(functions map[string]*i18nSourceFunction) {
	for _, function := range functions {
		params := i18nParameterIndexes(function.params)
		ast.Inspect(function.decl.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if i18nIsTranslationCall(call) {
				if id, ok := call.Args[0].(*ast.Ident); ok {
					if index, exists := params[id.Name]; exists {
						function.translated[index] = true
					}
				}
				return true
			}
			for _, argument := range i18nRawSinkArgIndexes(call) {
				if argument >= len(call.Args) {
					continue
				}
				if id, ok := call.Args[argument].(*ast.Ident); ok {
					if index, exists := params[id.Name]; exists {
						function.rawUI[index] = true
					}
				}
			}
			return true
		})
	}
}

func propagateI18nParameterUses(functions map[string]*i18nSourceFunction, calls []i18nSourceCall) {
	for {
		changed := false
		for _, site := range calls {
			owner, callee := functions[site.owner], functions[site.callee]
			if owner == nil || callee == nil {
				continue
			}
			params := i18nParameterIndexes(owner.params)
			max := min(len(site.call.Args), len(owner.params))
			for argument, expression := range site.call.Args {
				id, ok := expression.(*ast.Ident)
				if !ok {
					continue
				}
				ownerParam, ok := params[id.Name]
				if !ok || ownerParam >= max {
					continue
				}
				if callee.translated[argument] && !owner.translated[ownerParam] {
					owner.translated[ownerParam] = true
					changed = true
				}
				if callee.rawUI[argument] && !owner.rawUI[ownerParam] {
					owner.rawUI[ownerParam] = true
					changed = true
				}
			}
		}
		if !changed {
			return
		}
	}
}

func i18nParameterIndexes(params []string) map[string]int {
	indexes := map[string]int{}
	for index, name := range params {
		if name != "" && name != "_" {
			indexes[name] = index
		}
	}
	return indexes
}

// i18nLocalConstants resolves straightforward literal declarations and
// assignments. It deliberately does not attempt general data-flow analysis;
// unresolved values remain dynamic rather than being presented as covered.
func i18nLocalConstants(body *ast.BlockStmt) map[string]map[string]bool {
	values := map[string]map[string]bool{}
	add := func(name, text string) {
		if values[name] == nil {
			values[name] = map[string]bool{}
		}
		values[name][text] = true
	}
	ast.Inspect(body, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.AssignStmt:
			for i, lhs := range value.Lhs {
				id, ok := lhs.(*ast.Ident)
				if !ok || i >= len(value.Rhs) {
					continue
				}
				if text, ok := i18nStaticString(value.Rhs[i]); ok {
					add(id.Name, text)
				}
			}
		case *ast.ValueSpec:
			for i, id := range value.Names {
				if i < len(value.Values) {
					if text, ok := i18nStaticString(value.Values[i]); ok {
						add(id.Name, text)
					}
				}
			}
		}
		return true
	})
	return values
}

func i18nStaticString(expression ast.Expr) (string, bool) {
	switch value := expression.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return "", false
		}
		text, err := strconv.Unquote(value.Value)
		return text, err == nil
	case *ast.BinaryExpr:
		if value.Op != token.ADD {
			return "", false
		}
		left, ok := i18nStaticString(value.X)
		if !ok {
			return "", false
		}
		right, ok := i18nStaticString(value.Y)
		return left + right, ok
	case *ast.ParenExpr:
		return i18nStaticString(value.X)
	}
	return "", false
}

func i18nCallName(call *ast.CallExpr) string {
	switch function := call.Fun.(type) {
	case *ast.Ident:
		return function.Name
	case *ast.SelectorExpr:
		return function.Sel.Name
	}
	return ""
}

func i18nIsTranslationCall(call *ast.CallExpr) bool {
	name := i18nCallName(call)
	return (name == "tr" || name == "trf") && len(call.Args) > 0
}

func i18nContainsTranslationCall(expression ast.Expr) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok && i18nIsTranslationCall(call) {
			found = true
		}
		return !found
	})
	return found
}

func i18nRawSinkArgIndexes(call *ast.CallExpr) []int {
	switch i18nCallName(call) {
	case "Text", "Textf", "Button", "PrimaryButton", "Badge", "Field":
		return []int{1}
	case "Checkbox":
		return []int{2}
	case "Segmented":
		indexes := []int{}
		for index := 1; index < len(call.Args); index++ {
			indexes = append(indexes, index)
		}
		return indexes
	case "Label", "Tooltip", "Placeholder", "Item":
		return []int{0}
	}
	return nil
}

func i18nReceiverInfo(decl *ast.FuncDecl) (string, string) {
	if decl.Recv == nil || len(decl.Recv.List) == 0 {
		return "", ""
	}
	field := decl.Recv.List[0]
	name := ""
	if len(field.Names) > 0 {
		name = field.Names[0].Name
	}
	var receiver string
	switch value := field.Type.(type) {
	case *ast.Ident:
		receiver = value.Name
	case *ast.StarExpr:
		if id, ok := value.X.(*ast.Ident); ok {
			receiver = id.Name
		}
	}
	return receiver, name
}

func i18nFunctionKey(decl *ast.FuncDecl) string {
	receiver, _ := i18nReceiverInfo(decl)
	if receiver != "" {
		return receiver + "." + decl.Name.Name
	}
	return decl.Name.Name
}

// Function names are package-local. Prefixing their source directory prevents
// unrelated packages with the same helper name from sharing translation or
// raw-UI parameter metadata during the repository-wide analysis.
func i18nScopedFunctionKey(source i18nSourceFile, decl *ast.FuncDecl) string {
	return i18nScopedName(source.scope, i18nFunctionKey(decl))
}

func i18nScopedName(scope, name string) string {
	if scope == "" || scope == "." || name == "" {
		return name
	}
	return scope + "/" + name
}

func i18nCalleeKey(call *ast.CallExpr, owner *i18nSourceFunction) string {
	if id, ok := call.Fun.(*ast.Ident); ok {
		return id.Name
	}
	if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
		if id, ok := selector.X.(*ast.Ident); ok && owner != nil && owner.receiver != "" && id.Name == owner.receiverName {
			return owner.receiver + "." + selector.Sel.Name
		}
		return selector.Sel.Name
	}
	return ""
}

func i18nCompositeTypeName(expression ast.Expr) string {
	switch value := expression.(type) {
	case *ast.SelectorExpr:
		if pkg, ok := value.X.(*ast.Ident); ok {
			return pkg.Name + "." + value.Sel.Name
		}
	case *ast.Ident:
		return value.Name
	}
	return ""
}

func i18nCompositeFieldName(expression ast.Expr) string {
	if id, ok := expression.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func i18nHasUIProse(text string) bool {
	// Pure formats such as "%d × %d" are layout, not language. A format
	// mixed with words still needs review (for example, "%s arguments").
	text = formatDirective.ReplaceAllString(text, "")
	for _, r := range text {
		if r <= unicode.MaxASCII && unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func TestI18nStaticString(t *testing.T) {
	expression, err := parser.ParseExpr(`("part " + ` + "`two`" + `) + "!"`)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := i18nStaticString(expression); !ok || got != "part two!" {
		t.Fatalf("static string = %q, %v; want %q, true", got, ok, "part two!")
	}
	expression, err = parser.ParseExpr(`"prefix " + name`)
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := i18nStaticString(expression); ok {
		t.Fatalf("dynamic expression unexpectedly resolved to %q", got)
	}
}

func TestI18nSourceAnalysisConstantsWrappersAndRawSinks(t *testing.T) {
	const source = `package app
	func direct(name string) {
		local := "Local key"
		alias := local
		tr(local)
		tr(alias)
		trf("Formatted " + "key", 1)
		translate("Helper key")
		wrap("Wrapped key")
		literal := func(text string) { tr(text) }
		literal("Literal helper key")
		tr(name)
		ui.Text(c, "Raw text")
		ui.Text(c, tr("Wrapped raw text"))
		Box(c).Tooltip("Raw tooltip")
		paint("Raw helper")
		opts := mygo.OpenDialogOptions{Title: "Raw dialog", ButtonLabel: tr("Open")}
		state := editor.State{Label: "Raw editor"}
		_, _, _ = opts, state, c
	}
	func translate(text string) { tr(text) }
	func wrap(text string) { translate(text) }
	func paint(text string) { Box(c).Tooltip(text) }
	type first struct{}
	type second struct{}
	func (f *first) run() { f.show("First method") }
	func (f *first) show(text string) { tr(text) }
	func (f *second) run() { f.show("Second method must not be extracted") }
	func (f *second) show(text string) { println(text) }
	`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	analysis := analyzeI18nSources([]i18nSourceFile{{name: "fixture.go", file: file}}, fset)

	// Aliasing is deliberately outside the small local-constant model.
	for _, key := range []string{"Local key", "Formatted key", "Helper key", "Wrapped key", "Literal helper key", "First method"} {
		if len(analysis.keys[key]) == 0 {
			t.Errorf("translation key %q was not extracted; all keys: %#v", key, analysis.keys)
		}
	}
	const unrelatedMethodKey = "Second method must not be extracted"
	if len(analysis.keys[unrelatedMethodKey]) != 0 {
		t.Errorf("unexpected translation key %q", unrelatedMethodKey)
	}
	if analysis.dynamicDirectCalls != 5 { // unresolved aliases and translating parameters
		t.Errorf("dynamic direct calls = %d, want 5", analysis.dynamicDirectCalls)
	}
	for _, text := range []string{"Raw text", "Raw tooltip", "Raw helper", "Raw dialog", "Raw editor"} {
		if len(analysis.rawUI[text]) == 0 {
			t.Errorf("raw UI text %q was not extracted; all raw text: %#v", text, analysis.rawUI)
		}
	}
	if len(analysis.rawUI["Wrapped raw text"]) != 0 {
		t.Error("a translated UI argument was reported as raw")
	}
}
