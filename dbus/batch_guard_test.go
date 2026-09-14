package dbus

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A tea.Cmd returned where a tea.Msg is expected compiles cleanly, because
// tea.Msg is an empty interface. At runtime the loop only dispatches BatchMsg
// and sequenceMsg, so a returned Cmd is handed to Update, matches nothing, and
// is silently dropped along with everything it was going to run.
//
// That is how the DNS picker stopped refreshing the Known Networks table: the
// refresh command was there, packaged with tea.Batch, and never executed.
//
// Inside a func() tea.Msg the correct forms are tea.BatchMsg(cmds) and the
// message types themselves. tea.Batch and tea.Sequence are for values returned
// as commands, from Update or from a function whose result type is tea.Cmd.
func TestNoCommandConstructorsReturnedAsMessages(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, src, 0)
		if err != nil {
			t.Fatal(err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.FuncLit)
			if !ok || !returnsMsg(lit.Type) {
				return true
			}
			// Only look at this literal's own returns, not those of any
			// function literal nested inside it.
			ast.Inspect(lit.Body, func(inner ast.Node) bool {
				if _, nested := inner.(*ast.FuncLit); nested && inner != lit {
					return false
				}
				ret, ok := inner.(*ast.ReturnStmt)
				if !ok {
					return true
				}
				for _, res := range ret.Results {
					if name := commandConstructor(res); name != "" {
						t.Errorf("%s: returns tea.%s from a func() tea.Msg; "+
							"the runtime drops it. Use tea.BatchMsg(cmds) instead",
							fset.Position(ret.Pos()), name)
					}
				}
				return true
			})
			return true
		})
	}
}

// returnsMsg reports whether a signature yields exactly one tea.Msg.
func returnsMsg(ft *ast.FuncType) bool {
	if ft.Results == nil || len(ft.Results.List) != 1 {
		return false
	}
	sel, ok := ft.Results.List[0].Type.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "tea" && sel.Sel.Name == "Msg"
}

// commandConstructor names the tea command builder being called, if any.
func commandConstructor(e ast.Expr) string {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return ""
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "tea" {
		return ""
	}
	switch sel.Sel.Name {
	case "Batch", "Sequence", "Tick", "Every":
		return sel.Sel.Name
	}
	return ""
}
