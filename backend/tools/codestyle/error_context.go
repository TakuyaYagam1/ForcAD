package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type errorContextIssue struct {
	position token.Position
	message  string
}

type errorContextScope struct {
	body       *ast.BlockStmt
	parameters map[*ast.Field]bool
	imports    map[string]string
	dotImports map[string]bool
	set        *token.FileSet
	issues     []errorContextIssue
}

func checkErrorFile(path string) ([]errorContextIssue, error) {
	const location = "codestyle.checkErrorFile"
	if excludedErrorSource(path) {
		return nil, nil
	}
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s - read source: %w", location, err)
	}
	return checkErrorSource(path, source)
}

func excludedErrorSource(path string) bool {
	return strings.HasSuffix(path, "_test.go") || strings.Contains("/"+filepath.ToSlash(path), "/integration_test/")
}

func checkErrorSource(filename string, source []byte) ([]errorContextIssue, error) {
	const location = "codestyle.checkErrorSource"
	if excludedErrorSource(filename) {
		return nil, nil
	}
	set := token.NewFileSet()
	// Resolver objects distinguish imported calls from local fmt/errors values
	// and resolve captured location constants through nested closures.
	file, err := parser.ParseFile(set, filename, source, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("%s - parse source: %w", location, err)
	}
	if ast.IsGenerated(file) {
		return nil, nil
	}
	imports, dotImports := errorImports(file)
	var issues []errorContextIssue
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok {
			issues = append(issues, inspectErrorFunction(function.Body, function.Type, imports, dotImports, set)...)
			continue
		}
		// Package-level errors.New calls create sentinels and are excluded.
		// Function-valued initializers still have normal runtime bodies.
		ast.Inspect(declaration, func(node ast.Node) bool {
			if function, ok := node.(*ast.FuncLit); ok {
				issues = append(issues, inspectErrorFunction(function.Body, function.Type, imports, dotImports, set)...)
				return false
			}
			return true
		})
	}
	return issues, nil
}

func errorImports(file *ast.File) (map[string]string, map[string]bool) {
	imports := make(map[string]string)
	dotImports := make(map[string]bool)
	for _, item := range file.Imports {
		path, _ := strconv.Unquote(item.Path.Value)
		name := filepath.Base(path)
		if item.Name != nil {
			name = item.Name.Name
		}
		if name == "." {
			dotImports[path] = true
		} else {
			imports[name] = path
		}
	}
	return imports, dotImports
}

func inspectErrorFunction(
	body *ast.BlockStmt,
	signature *ast.FuncType,
	imports map[string]string,
	dotImports map[string]bool,
	set *token.FileSet,
) []errorContextIssue {
	if body == nil {
		return nil
	}
	scope := &errorContextScope{
		body:       body,
		parameters: make(map[*ast.Field]bool),
		imports:    imports,
		dotImports: dotImports,
		set:        set,
	}
	scope.addParameters(signature)
	ast.Inspect(body, func(node ast.Node) bool {
		if function, ok := node.(*ast.FuncLit); ok {
			scope.addParameters(function.Type)
		}
		if call, ok := node.(*ast.CallExpr); ok {
			scope.checkConstructor(call)
		}
		return true
	})
	return scope.issues
}

func (scope *errorContextScope) addParameters(signature *ast.FuncType) {
	if signature.Params == nil {
		return
	}
	for _, field := range signature.Params.List {
		if kind, ok := field.Type.(*ast.Ident); ok && kind.Name == "string" {
			scope.parameters[field] = true
		}
	}
}

func (scope *errorContextScope) checkConstructor(call *ast.CallExpr) {
	switch scope.constructor(call.Fun) {
	case "errors.New":
		scope.issue(call, "runtime errors.New must use fmt.Errorf with a local location constant")
	case "fmt.Errorf":
		if len(call.Args) < 2 || !scope.validLocation(call.Args[1]) {
			scope.issue(call, "fmt.Errorf must use a local const location or an inherited location string parameter")
			return
		}
		message, ok := constantString(call.Args[0], make(map[*ast.Ident]bool))
		if !ok || !strings.HasPrefix(message, "%s - ") {
			scope.issue(call, "fmt.Errorf format must start with \"%s - \" and use location as its first substitution")
		}
	}
}

func (scope *errorContextScope) constructor(expression ast.Expr) string {
	switch function := expression.(type) {
	case *ast.SelectorExpr:
		if qualifier, ok := function.X.(*ast.Ident); ok && qualifier.Obj == nil {
			return knownErrorConstructor(scope.imports[qualifier.Name], function.Sel.Name)
		}
	case *ast.Ident:
		if function.Obj == nil {
			for _, path := range []string{"fmt", "errors"} {
				if scope.dotImports[path] {
					if name := knownErrorConstructor(path, function.Name); name != "" {
						return name
					}
				}
			}
		}
	}
	return ""
}

func knownErrorConstructor(path, name string) string {
	if path == "fmt" && name == "Errorf" || path == "errors" && name == "New" {
		return path + "." + name
	}
	return ""
}

func (scope *errorContextScope) validLocation(expression ast.Expr) bool {
	name, ok := expression.(*ast.Ident)
	if !ok || name.Name != "location" || name.Obj == nil {
		return false
	}
	if name.Obj.Kind == ast.Con {
		if declaration, ok := name.Obj.Decl.(*ast.ValueSpec); ok {
			value, valid := constantString(name, make(map[*ast.Ident]bool))
			return declaration.Pos() > scope.body.Pos() && declaration.End() < scope.body.End() && valid && strings.TrimSpace(value) != ""
		}
	}
	if parameter, ok := name.Obj.Decl.(*ast.Field); ok {
		return scope.parameters[parameter]
	}
	return false
}

func (scope *errorContextScope) issue(call *ast.CallExpr, message string) {
	scope.issues = append(scope.issues, errorContextIssue{
		position: scope.set.Position(call.Pos()),
		message:  message,
	})
}

func constantString(expression ast.Expr, seen map[*ast.Ident]bool) (string, bool) {
	switch value := expression.(type) {
	case *ast.BasicLit:
		if value.Kind == token.STRING {
			text, err := strconv.Unquote(value.Value)
			return text, err == nil
		}
	case *ast.BinaryExpr:
		if value.Op == token.ADD {
			left, leftOK := constantString(value.X, seen)
			right, rightOK := constantString(value.Y, seen)
			return left + right, leftOK && rightOK
		}
	case *ast.Ident:
		return constantIdentifier(value, seen)
	}
	return "", false
}

func constantIdentifier(name *ast.Ident, seen map[*ast.Ident]bool) (string, bool) {
	if name.Obj == nil || name.Obj.Kind != ast.Con {
		return "", false
	}
	declaration, ok := name.Obj.Decl.(*ast.ValueSpec)
	if !ok {
		return "", false
	}
	for index, field := range declaration.Names {
		if field.Name == name.Name && index < len(declaration.Values) {
			if seen[field] {
				return "", false
			}
			seen[field] = true
			defer delete(seen, field)
			return constantString(declaration.Values[index], seen)
		}
	}
	return "", false
}
