package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"strings"
	"testing"
)

// bubbleup's third constructor argument is a time.Duration, so a bare integer
// literal is that many nanoseconds. A 10 written there reads as ten seconds
// and means ten nanoseconds, which the first 100ms tick finds already expired:
// the alert is on screen for one frame, at the 0.3 blend it opens on, and then
// gone. That is indistinguishable from a fade that never ran, which is what it
// was mistaken for.
//
// Nothing about the type system catches it -- 10 is a valid untyped constant
// for a Duration -- and nothing about reading the line catches it either. So
// every call site is checked here, rather than the one that was wrong, because
// the next one written is as likely to say 10 as all three of these were.
func TestAlertDurationsAreNotBareNanoseconds(t *testing.T) {
	for _, path := range goFilesInPackage(t) {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "NewAlertModel" {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); !ok || pkg.Name != "bubbleup" {
				return true
			}
			if len(call.Args) < 3 {
				return true
			}
			lit, ok := call.Args[2].(*ast.BasicLit)
			if !ok || lit.Kind != token.INT {
				// Anything built out of time.Second and friends is fine, which
				// is the whole of what this test is asking for.
				return true
			}
			// Zero is the one integer that means what it says in any unit, and
			// the expiry tests pass it deliberately to expire immediately.
			if lit.Value == "0" {
				return true
			}
			t.Errorf("%s: NewAlertModel duration %s is a bare integer, so %s nanoseconds; "+
				"multiply by time.Second or the alert dies on the first tick",
				fset.Position(lit.Pos()), lit.Value, lit.Value)
			return true
		})
	}
}

// goFilesInPackage lists the root package's own source, tests included, since
// the wrong unit is as easy to write in a test helper as in the program and a
// test built on a dead alert quietly stops checking anything.
func goFilesInPackage(t *testing.T) []string {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	var paths []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".go") {
			paths = append(paths, e.Name())
		}
	}
	if len(paths) == 0 {
		t.Fatal("no Go files found in the package directory")
	}
	return paths
}
