package main

import (
	"bytes"
	"go/ast"
	"strings"
)

// A trailing nolint on a compact statement must keep covering that statement
// when its literal or body expands. A leading directive has stable scope.
func (changes *layout) preserveDirectives(file *ast.File) {
	var statements []ast.Stmt
	ast.Inspect(file, func(node ast.Node) bool {
		statement, ok := node.(ast.Stmt)
		if !ok {
			return true
		}
		switch statement.(type) {
		case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
			return true
		default:
			statements = append(statements, statement)
			return true
		}
	})
	for _, group := range file.Comments {
		for _, comment := range group.List {
			if isNolint(comment.Text) {
				changes.moveDirective(comment, statements)
			}
		}
	}
}

func isNolint(text string) bool {
	return text == "//nolint" || strings.HasPrefix(text, "//nolint:") || strings.HasPrefix(text, "//nolint ")
}

func (changes *layout) moveDirective(comment *ast.Comment, statements []ast.Stmt) {
	commentStart := changes.file.Offset(comment.Pos())
	var selected ast.Stmt
	for _, statement := range statements {
		end := changes.file.Offset(statement.End())
		if end > commentStart || len(bytes.Trim(changes.source[end:commentStart], " \t")) != 0 {
			continue
		}
		if selected == nil || statement.Pos() > selected.Pos() {
			selected = statement
		}
	}
	if selected == nil {
		return
	}
	start, end := changes.file.Offset(selected.Pos()), changes.file.Offset(selected.End())
	if !changes.multilineStatement(start, end) {
		return
	}
	changes.prefix[start] += comment.Text + "\n"
	changes.remove[commentStart] = changes.file.Offset(comment.End())
}

func (changes *layout) multilineStatement(start, end int) bool {
	if bytes.ContainsRune(changes.source[start:end], '\n') {
		return true
	}
	for position := range changes.breaks {
		if position > start && position < end {
			return true
		}
	}
	return false
}
