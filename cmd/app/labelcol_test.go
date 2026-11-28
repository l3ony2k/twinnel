package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The label-cut bug class (Windows QA round 4): a label drawn in a
// fixed-width column as one line is cut with "…" as soon as the font is
// wider than the one the column was sized with (Microsoft YaHei UI). The
// kv rows' fix went into kvLabel, but 停止使用 drew its own labels and
// kept the bug. Every label column goes through kvLabel (the kv rows'
// shared column) or colLabel (any other fixed width), which wrap instead.
// This test fails on a ui.Text chain that sets a Width and SingleLine
// without middle truncation (a path or value column, not a label), and on
// one that sizes itself to labelCol() outside kvLabel.
func TestNoFixedWidthSingleLineLabels(t *testing.T) {
	// Functions whose fixed-width single-line text is not a label, and why.
	notLabels := map[string]string{
		"colLabel":       "the fix itself",
		"kvLabel":        "the fix itself",
		"renderCoverage": "renders the tray speed's glyphs into an atlas cell",
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[token.Pos]bool{} // one report per chain (inner calls are prefixes)
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			root, methods := textChain(call)
			if root == nil || seen[root.Pos()] {
				return true
			}
			seen[root.Pos()] = true
			fn := enclosingFunc(f, call.Pos())
			switch {
			case methods["Width"] && methods["SingleLine"] && !methods["TruncateMiddle"] && notLabels[fn] == "":
				t.Errorf("%s: a label in a fixed-width column drawn as one line is cut on a wider font; use kvLabel or colLabel", fset.Position(root.Pos()))
			case methods["labelCol"] && fn != "kvLabel":
				t.Errorf("%s: a label sized to labelCol() is a kv label; use kvLabel", fset.Position(root.Pos()))
			}
			return true
		})
	}
}

// textChain finds the ui.Text call a method chain starts from (through
// one-argument wrappers such as mono(ui.Text(…))) and the methods called
// on it, outermost first. A call to labelCol in a Width argument counts
// as the method "labelCol". root is nil for any other chain.
func textChain(call *ast.CallExpr) (root *ast.CallExpr, methods map[string]bool) {
	methods = map[string]bool{}
	var e ast.Expr = call
	for {
		c, ok := e.(*ast.CallExpr)
		if !ok {
			return nil, nil
		}
		switch fun := c.Fun.(type) {
		case *ast.SelectorExpr:
			if id, ok := fun.X.(*ast.Ident); ok && id.Name == "ui" {
				if fun.Sel.Name == "Text" {
					return c, methods
				}
				return nil, nil
			}
			methods[fun.Sel.Name] = true
			if fun.Sel.Name == "Width" && len(c.Args) == 1 && callsLabelCol(c.Args[0]) {
				methods["labelCol"] = true
			}
			e = fun.X
		case *ast.Ident:
			if len(c.Args) != 1 {
				return nil, nil
			}
			e = c.Args[0]
		default:
			return nil, nil
		}
	}
}

func callsLabelCol(e ast.Expr) bool {
	found := false
	ast.Inspect(e, func(n ast.Node) bool {
		if c, ok := n.(*ast.CallExpr); ok {
			if id, ok := c.Fun.(*ast.Ident); ok && id.Name == "labelCol" {
				found = true
			}
		}
		return !found
	})
	return found
}

// enclosingFunc is the name of the top-level function declaring pos.
func enclosingFunc(f *ast.File, pos token.Pos) string {
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Pos() <= pos && pos < fd.End() {
			return fd.Name.Name
		}
	}
	return ""
}
