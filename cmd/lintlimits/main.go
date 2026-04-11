package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type violation struct {
	path    string
	line    int
	message string
}

func main() {
	var (
		maxFileLines = flag.Int("max-file-lines", 750, "maximum allowed lines per file")
		maxFuncLines = flag.Int("max-func-lines", 150, "maximum allowed lines per function")
		root         = flag.String("root", ".", "root directory to scan")
	)
	flag.Parse()

	violations, err := check(*root, *maxFileLines, *maxFuncLines)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lintlimits: %v\n", err)
		os.Exit(1)
	}

	if len(violations) == 0 {
		return
	}

	for _, item := range violations {
		fmt.Fprintf(os.Stderr, "%s:%d: %s\n", item.path, item.line, item.message)
	}
	os.Exit(1)
}

func check(root string, maxFileLines int, maxFuncLines int) ([]violation, error) {
	fset := token.NewFileSet()
	violations := make([]violation, 0)

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "vendor", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		if fileLines := countLines(content); fileLines > maxFileLines {
			violations = append(violations, violation{
				path:    path,
				line:    1,
				message: fmt.Sprintf("file has %d lines, max allowed is %d", fileLines, maxFileLines),
			})
		}

		node, err := parser.ParseFile(fset, path, content, parser.SkipObjectResolution)
		if err != nil {
			return err
		}

		for _, decl := range node.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			start := fset.Position(fn.Pos()).Line
			end := fset.Position(fn.End()).Line
			lines := end - start + 1
			if lines <= maxFuncLines {
				continue
			}

			violations = append(violations, violation{
				path:    path,
				line:    start,
				message: fmt.Sprintf("function %s has %d lines, max allowed is %d", functionName(fn), lines, maxFuncLines),
			})
		}

		return nil
	})

	return violations, err
}

func countLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	return strings.Count(string(content), "\n") + 1
}

func functionName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}

	switch expr := fn.Recv.List[0].Type.(type) {
	case *ast.Ident:
		return expr.Name + "." + fn.Name.Name
	case *ast.StarExpr:
		if ident, ok := expr.X.(*ast.Ident); ok {
			return ident.Name + "." + fn.Name.Name
		}
	}

	return fn.Name.Name
}
