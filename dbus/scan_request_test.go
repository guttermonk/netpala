package dbus

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Asking NetworkManager for a scan and reading what it already found are two
// different jobs, and GetScanResults used to do both. The debounce that calls
// it runs off AccessPointAdded/Removed, so every read asked for a scan, the
// scan moved the access point list, and the churn arrived back as more
// signals. Which function asks is therefore a property worth pinning rather
// than a detail: a second asker anywhere on a signal path starts it again.
func TestOnlyRequestScanAsksForAScan(t *testing.T) {
	askers := functionsCalling(t, "RequestScan")

	if len(askers) != 1 || askers[0] != "RequestScan" {
		t.Errorf("the D-Bus RequestScan call appears in %v, want only RequestScan", askers)
	}
}

// And the timer path still asks, or New Networks stops moving: a connected
// device scans rarely on its own, and nothing else requests one unprompted.
func TestThePeriodicRefreshStillRequestsAScan(t *testing.T) {
	for _, path := range packageFiles(t) {
		for _, fn := range functionDecls(t, path) {
			if fn.Name.Name != "RefreshAllData" {
				continue
			}
			found := false
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "RequestScan" {
					found = true
				}
				return true
			})
			if !found {
				t.Error("RefreshAllData no longer calls RequestScan; the scan list will go stale while connected")
			}
			return
		}
	}
	t.Fatal("RefreshAllData not found")
}

// functionsCalling names every function in the package whose body mentions the
// given D-Bus method in a string, sorted.
func functionsCalling(t *testing.T, method string) []string {
	t.Helper()

	var names []string
	for _, path := range packageFiles(t) {
		for _, fn := range functionDecls(t, path) {
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				if strings.Contains(lit.Value, "."+method) {
					names = append(names, fn.Name.Name)
					return false
				}
				return true
			})
		}
	}
	sort.Strings(names)
	return names
}

func packageFiles(t *testing.T) []string {
	t.Helper()

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, path := range files {
		if !strings.HasSuffix(path, "_test.go") {
			out = append(out, path)
		}
	}
	return out
}

func functionDecls(t *testing.T, path string) []*ast.FuncDecl {
	t.Helper()

	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, src, 0)
	if err != nil {
		t.Fatal(err)
	}

	var out []*ast.FuncDecl
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
			out = append(out, fn)
		}
	}
	return out
}
