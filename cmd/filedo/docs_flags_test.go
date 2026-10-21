package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The drift gate for the documented check options (DOC-INTERNAL-QUALITY
// rule 4, SP-0061).
//
// The READMEs print the flags "filedo check" accepts and the FILEDO_CHECK_*
// variables each one mirrors, and filedo_check's README prints the options it
// hands over. All three lists are written by hand, so a flag added to the
// parser without a line in them is a feature nobody can find, and a line whose
// flag is gone is a promise the code does not keep. The truth is read from
// the source, not from --help: the flag set is every fs.<Kind>("name", ..)
// call in HandleCheckArgs, and the variables are every string literal that is
// exactly a FILEDO_CHECK_* name.

var (
	docFlagRe  = regexp.MustCompile("`--([a-z0-9][a-z0-9-]*)")
	docEnvRe   = regexp.MustCompile(`FILEDO_CHECK_[A-Z0-9_]+`)
	envNameRe  = regexp.MustCompile(`^FILEDO_CHECK_[A-Z0-9_]+$`)
	flagLineRe = regexp.MustCompile("^\\s*- `--")
	optRowRe   = regexp.MustCompile("^\\|\\s*`--")
)

// checkParserFlags returns the flag names HandleCheckArgs defines.
func checkParserFlags(t *testing.T) map[string]bool {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "check.go", nil, 0)
	if err != nil {
		t.Fatalf("check.go cannot be parsed: %v", err)
	}
	flags := map[string]bool{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "HandleCheckArgs" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) == 0 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); !ok || x.Name != "fs" {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			if name, err := strconv.Unquote(lit.Value); err == nil {
				flags[name] = true
			}
			return true
		})
	}
	if len(flags) == 0 {
		t.Fatal("DOCS_FLAGS_NO_PARSER: no fs.<Kind>(\"name\", ..) call found in HandleCheckArgs - the gate would pass on nothing")
	}
	return flags
}

// checkEnvNames returns every FILEDO_CHECK_* name the package's code uses.
func checkEnvNames(t *testing.T) map[string]bool {
	t.Helper()
	paths, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	fset := token.NewFileSet()
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatalf("%s cannot be parsed: %v", p, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				if s, err := strconv.Unquote(lit.Value); err == nil && envNameRe.MatchString(s) {
					names[s] = true
				}
			}
			return true
		})
	}
	if len(names) == 0 {
		t.Fatal("DOCS_FLAGS_NO_ENV: no FILEDO_CHECK_* literal found in the package - the gate would pass on nothing")
	}
	return names
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestDocs_CheckFlagListMatchesTheParser holds every README that prints the
// check flag list to the parser, both ways, and to the variables the code
// reads.
func TestDocs_CheckFlagListMatchesTheParser(t *testing.T) {
	root := repoRoot(t)
	flags := checkParserFlags(t)
	envs := checkEnvNames(t)

	readmes, err := filepath.Glob(filepath.Join(root, "README*.md"))
	if err != nil {
		t.Fatal(err)
	}
	carriers := 0
	for _, path := range readmes {
		rel := filepath.Base(path)
		docFlags, docEnvs := map[string]bool{}, map[string]bool{}
		for _, line := range strings.Split(readSurface(t, root, rel), "\n") {
			if !flagLineRe.MatchString(line) {
				continue
			}
			for _, m := range docFlagRe.FindAllStringSubmatch(line, -1) {
				docFlags[m[1]] = true
			}
			for _, m := range docEnvRe.FindAllString(line, -1) {
				docEnvs[m] = true
			}
		}
		// A README carries the list when its flag lines name the variables;
		// the other locales document no check flags at all.
		if len(docEnvs) == 0 {
			continue
		}
		carriers++
		for _, name := range sortedKeys(flags) {
			if !docFlags[name] {
				t.Errorf("DOCS_FLAG_UNDOCUMENTED: filedo check accepts --%s but %s does not list it", name, rel)
			}
		}
		for _, name := range sortedKeys(docFlags) {
			if !flags[name] {
				t.Errorf("DOCS_FLAG_UNKNOWN: %s lists --%s, which filedo check does not accept", rel, name)
			}
		}
		for _, name := range sortedKeys(envs) {
			if !docEnvs[name] {
				t.Errorf("DOCS_ENV_UNDOCUMENTED: the code reads %s but %s's flag list does not name it", name, rel)
			}
		}
		for _, name := range sortedKeys(docEnvs) {
			if !envs[name] {
				t.Errorf("DOCS_ENV_UNKNOWN: %s names %s, which the code never reads", rel, name)
			}
		}
	}
	if carriers == 0 {
		t.Fatal("DOCS_FLAGS_NO_CARRIER: no README carries the check flag list - the gate would pass on nothing")
	}
}

// launcherCheckOptions returns the keys of cmd/filedo-check's checkOptions
// map - the options the launcher accepts. That module is separate and its own
// tests are not part of the build gate, so its source is read from here.
func launcherCheckOptions(t *testing.T, root string) map[string]bool {
	t.Helper()
	path := filepath.Join(root, "cmd", "filedo-check", "main.go")
	f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("%s cannot be parsed: %v", path, err)
	}
	opts := map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok || len(spec.Names) != 1 || spec.Names[0].Name != "checkOptions" || len(spec.Values) != 1 {
			return true
		}
		if lit, ok := spec.Values[0].(*ast.CompositeLit); ok {
			for _, elt := range lit.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					if key, ok := kv.Key.(*ast.BasicLit); ok && key.Kind == token.STRING {
						if s, err := strconv.Unquote(key.Value); err == nil {
							opts[strings.TrimPrefix(s, "--")] = true
						}
					}
				}
			}
		}
		return false
	})
	if len(opts) == 0 {
		t.Fatalf("DOCS_LAUNCHER_NO_OPTIONS: no checkOptions map found in %s - the gate would pass on nothing", path)
	}
	return opts
}

// TestDocs_CheckLauncherOptionTable holds filedo_check's option table to the
// launcher's checkOptions both ways, and each option to the check parser: the
// launcher hands an option over under the same name, so one the parser does
// not know would be refused by filedo.exe after the launcher accepted it.
func TestDocs_CheckLauncherOptionTable(t *testing.T) {
	root := repoRoot(t)
	flags := checkParserFlags(t)
	accepted := launcherCheckOptions(t, root)
	rel := filepath.Join("cmd", "filedo-check", "README.md")
	documented := map[string]bool{}
	for _, line := range strings.Split(readSurface(t, root, rel), "\n") {
		if !optRowRe.MatchString(line) {
			continue
		}
		cell := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(line), "|"), "|", 2)[0]
		for _, m := range docFlagRe.FindAllStringSubmatch(cell, -1) {
			documented[m[1]] = true
		}
	}
	if len(documented) == 0 {
		t.Fatalf("DOCS_LAUNCHER_NO_TABLE: %s has no option table - the gate would pass on nothing", rel)
	}
	for _, name := range sortedKeys(documented) {
		if !flags[name] {
			t.Errorf("DOCS_LAUNCHER_OPTION_UNKNOWN: %s lists --%s, which filedo check does not accept", rel, name)
		}
		if !accepted[name] {
			t.Errorf("DOCS_LAUNCHER_OPTION_REFUSED: %s lists --%s, which the launcher refuses", rel, name)
		}
	}
	for _, name := range sortedKeys(accepted) {
		if !documented[name] {
			t.Errorf("DOCS_LAUNCHER_OPTION_UNDOCUMENTED: the launcher accepts --%s but %s does not list it", name, rel)
		}
	}
}
