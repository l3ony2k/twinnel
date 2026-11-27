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

// A pull-down (操作 ▾, ⋯, 应急恢复) wears the push-button face, as the
// buttons beside it and in the neighbouring cards' headings do: every
// ui.MenuButton goes through pushMenu. A bare one draws MyGo's lighter
// outlined face (the 总览 mihomo card's 操作, 2026-11-27).
func TestMenuButtonsHavePushFace(t *testing.T) {
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
			if !ok || fn.Name.Name == "pushMenu" {
				continue
			}
			ast.Inspect(fn, func(n ast.Node) bool {
				sel, ok := n.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "MenuButton" {
					return true
				}
				if id, ok := sel.X.(*ast.Ident); ok && id.Name == "ui" {
					t.Errorf("%s: %s calls ui.MenuButton; use pushMenu", fset.Position(sel.Pos()), fn.Name.Name)
				}
				return true
			})
		}
	}
}
