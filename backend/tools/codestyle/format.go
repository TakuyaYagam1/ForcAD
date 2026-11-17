package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/scanner"
	"go/token"
	"reflect"
	"slices"
)

type sourceToken struct {
	start int
	end   int
	kind  token.Token
	text  string
}

type layout struct {
	source []byte
	file   *token.File
	tokens []sourceToken
	breaks map[int]bool
	commas map[int]bool
	remove map[int]int
	prefix map[int]string
}

// formatSource changes only layout separators identified by the parsed syntax.
// Comparing the resulting syntax trees catches any accidental semantic change.
func formatSource(filename string, source []byte) ([]byte, error) {
	const location = "codestyle.formatSource"
	canonical, err := format.Source(source)
	if err != nil {
		return nil, fmt.Errorf("%s - normalize source: %w", location, err)
	}
	set := token.NewFileSet()
	original, err := parser.ParseFile(set, filename, canonical, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("%s - parse source: %w", location, err)
	}
	changes := newLayout(canonical, set.File(original.Pos()))
	ast.Inspect(original, func(node ast.Node) bool {
		changes.expand(node)
		return true
	})
	changes.preserveDirectives(original)
	formatted, err := format.Source(changes.apply())
	if err != nil {
		return nil, fmt.Errorf("%s - format expanded source: %w", location, err)
	}
	updated, err := parser.ParseFile(token.NewFileSet(), filename, formatted, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("%s - parse expanded source: %w", location, err)
	}
	if syntaxFingerprint(original) != syntaxFingerprint(updated) {
		return nil, fmt.Errorf("%s - expanded source changed the syntax tree", location)
	}
	return formatted, nil
}

func newLayout(source []byte, file *token.File) *layout {
	changes := &layout{
		source: source,
		file:   file,
		breaks: make(map[int]bool),
		commas: make(map[int]bool),
		remove: make(map[int]int),
		prefix: make(map[int]string),
	}
	var scan scanner.Scanner
	scan.Init(file, source, nil, scanner.ScanComments)
	for {
		position, kind, literal := scan.Scan()
		if kind == token.EOF {
			break
		}
		start := file.Offset(position)
		changes.tokens = append(changes.tokens, sourceToken{
			start: start,
			end:   start + len(literal),
			kind:  kind,
			text:  literal,
		})
	}
	return changes
}

func (changes *layout) expand(node ast.Node) {
	switch item := node.(type) {
	case *ast.BlockStmt:
		changes.statements(item.Lbrace+1, item.Rbrace, item.List)
	case *ast.StructType:
		changes.fields(item.Fields)
	case *ast.InterfaceType:
		changes.fields(item.Methods)
	case *ast.FuncType:
		changes.parameters(item.Params)
	case *ast.CaseClause:
		changes.statements(item.Colon+1, token.NoPos, item.Body)
	case *ast.CommClause:
		changes.statements(item.Colon+1, token.NoPos, item.Body)
	case *ast.CompositeLit:
		changes.keyedLiteral(item)
	}
}

func (changes *layout) statements(start, end token.Pos, statements []ast.Stmt) {
	previous := start
	for _, statement := range statements {
		changes.removeSemicolons(previous, statement.Pos())
		changes.newlineBefore(statement.Pos())
		previous = statement.End()
	}
	if end.IsValid() {
		changes.removeSemicolons(previous, end)
		changes.newlineBefore(end)
	}
}

func (changes *layout) fields(fields *ast.FieldList) {
	if fields == nil || !fields.Opening.IsValid() || !fields.Closing.IsValid() {
		return
	}
	if len(fields.List) == 0 {
		changes.compactEmpty(fields.Opening, fields.Closing)
		return
	}
	previous := fields.Opening + 1
	for _, field := range fields.List {
		changes.removeSemicolons(previous, field.Pos())
		changes.newlineBefore(field.Pos())
		previous = field.End()
	}
	changes.removeSemicolons(previous, fields.Closing)
	changes.newlineBefore(fields.Closing)
}

func (changes *layout) keyedLiteral(literal *ast.CompositeLit) {
	if len(literal.Elts) == 0 {
		changes.compactEmpty(literal.Lbrace, literal.Rbrace)
		return
	}
	for _, element := range literal.Elts {
		if _, keyed := element.(*ast.KeyValueExpr); !keyed {
			return
		}
	}
	for _, element := range literal.Elts {
		changes.newlineBefore(element.Pos())
	}
	last := literal.Elts[len(literal.Elts)-1]
	if !changes.hasToken(last.End(), literal.Rbrace, token.COMMA) {
		changes.commas[changes.file.Offset(last.End())] = true
	}
	changes.newlineBefore(literal.Rbrace)
}

func (changes *layout) compactEmpty(opening, closing token.Pos) {
	start := changes.file.Offset(opening + 1)
	end := changes.file.Offset(closing)
	if start < end && len(bytes.TrimSpace(changes.source[start:end])) == 0 {
		changes.remove[start] = end
	}
}

func (changes *layout) newlineBefore(position token.Pos) {
	offset := changes.file.Offset(position)
	start := bytes.LastIndexByte(changes.source[:offset], '\n') + 1
	if len(bytes.Trim(changes.source[start:offset], " \t\r")) == 0 {
		return
	}
	changes.breaks[offset] = true
}

func (changes *layout) removeSemicolons(start, end token.Pos) {
	left, right := changes.file.Offset(start), changes.file.Offset(end)
	for _, item := range changes.tokensBetween(left, right) {
		if item.kind == token.SEMICOLON && item.text == ";" {
			changes.remove[item.start] = item.end
		}
	}
}

func (changes *layout) hasToken(start, end token.Pos, kind token.Token) bool {
	left, right := changes.file.Offset(start), changes.file.Offset(end)
	return slices.ContainsFunc(changes.tokensBetween(left, right), func(item sourceToken) bool {
		return item.kind == kind
	})
}

func (changes *layout) tokensBetween(start, end int) []sourceToken {
	first, _ := slices.BinarySearchFunc(changes.tokens, start, func(item sourceToken, offset int) int {
		return item.start - offset
	})
	last, _ := slices.BinarySearchFunc(changes.tokens, end, func(item sourceToken, offset int) int {
		return item.start - offset
	})
	return changes.tokens[first:last]
}

func (changes *layout) apply() []byte {
	var result bytes.Buffer
	for index := 0; index <= len(changes.source); index++ {
		if changes.commas[index] {
			result.WriteByte(',')
		}
		if changes.breaks[index] {
			result.WriteByte('\n')
		}
		result.WriteString(changes.prefix[index])
		if end, removed := changes.remove[index]; removed {
			index = end - 1
			continue
		}
		if index < len(changes.source) {
			result.WriteByte(changes.source[index])
		}
	}
	return result.Bytes()
}

func syntaxFingerprint(file *ast.File) string {
	var result bytes.Buffer
	_ = ast.Fprint(&result, nil, file, func(name string, value reflect.Value) bool {
		if value.Type() == reflect.TypeFor[token.Pos]() {
			return false
		}
		switch name {
		case "Doc", "Comment", "Comments", "Scope", "Unresolved":
			return false
		default:
			return true
		}
	})
	return result.String()
}
