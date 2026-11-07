package main

import (
	"bytes"
	"go/ast"
	"go/token"
)

const inlineParameterLimit = 3

func (changes *layout) parameters(parameters *ast.FieldList) {
	if parameters == nil || !parameters.Opening.IsValid() || !parameters.Closing.IsValid() {
		return
	}
	if parameterCount(parameters) > inlineParameterLimit {
		changes.expandParameters(parameters)
		return
	}
	// A line comment requires its newline. Preserve all parameter comments and
	// their placement rather than relocating documentation to force one line.
	if changes.hasToken(parameters.Opening+1, parameters.Closing, token.COMMENT) {
		return
	}
	changes.compactParameters(parameters)
}

func parameterCount(parameters *ast.FieldList) int {
	count := 0
	for _, field := range parameters.List {
		count += max(1, len(field.Names))
	}
	return count
}

func (changes *layout) expandParameters(parameters *ast.FieldList) {
	for _, field := range parameters.List {
		changes.compactParameterNames(field)
		changes.newlineBefore(field.Pos())
	}
	last := parameters.List[len(parameters.List)-1]
	if !changes.hasToken(last.End(), parameters.Closing, token.COMMA) {
		changes.commas[changes.file.Offset(last.End())] = true
	}
	changes.newlineBefore(parameters.Closing)
}

func (changes *layout) compactParameters(parameters *ast.FieldList) {
	previous := parameters.Opening + 1
	separator := ""
	for _, field := range parameters.List {
		changes.parameterGap(previous, field.Pos(), separator)
		changes.compactParameterNames(field)
		previous = field.End()
		separator = ", "
	}
	changes.parameterGap(previous, parameters.Closing, "")
}

func (changes *layout) compactParameterNames(field *ast.Field) {
	if changes.hasToken(field.Pos(), field.Type.Pos(), token.COMMENT) {
		return
	}
	for index, name := range field.Names {
		if index > 0 {
			changes.parameterGap(field.Names[index-1].End(), name.Pos(), ", ")
		}
	}
	if len(field.Names) > 0 {
		changes.parameterGap(field.Names[len(field.Names)-1].End(), field.Type.Pos(), " ")
	}
}

func (changes *layout) parameterGap(start, end token.Pos, replacement string) {
	left, right := changes.file.Offset(start), changes.file.Offset(end)
	if left >= right || bytes.Equal(changes.source[left:right], []byte(replacement)) {
		return
	}
	// Only separators are rewritten. Nested function signatures, anonymous
	// types, comments and string contents are formatted by their own AST nodes.
	if len(bytes.Trim(changes.source[left:right], " \t\r\n,")) != 0 {
		return
	}
	changes.remove[left] = right
	changes.prefix[left] = replacement
}
