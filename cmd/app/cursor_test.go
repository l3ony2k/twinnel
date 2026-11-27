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

// The pointing hand is for links only (AGENTS.md, design rules): buttons,
// rows, disclosures, tiles, toggles and menus keep the arrow, as in
// Finder and System Settings. ui.Link brings its own hand; nothing else
// in the app may ask for one. A real link that is not a ui.Link goes in a
// function named in linkCursorOK.
var linkCursorOK = map[string]bool{}

func TestPointingHandOnlyOnLinks(t *testing.T) {
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
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || linkCursorOK[fn.Name.Name] {
				continue
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "CursorPointer" {
					t.Errorf("%s: %s sets the pointing hand; only links may (use the arrow)", fset.Position(sel.Pos()), fn.Name.Name)
				}
				return true
			})
		}
	}
}

// The diffs draw their own folded-lines bar: MyGo's gives its buttons the
// hand.
func TestDiffSeparatorIsOurs(t *testing.T) {
	if newDiff("a\n", "b\n").RenderSeparator == nil {
		t.Fatal("newDiff uses MyGo's DefaultSeparator, whose buttons show the pointing hand")
	}
}
