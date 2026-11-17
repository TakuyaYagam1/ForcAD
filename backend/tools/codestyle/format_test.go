package main

import (
	"bytes"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestExpandedStatementsFieldsAndKeyedLiterals(t *testing.T) {
	source := "package fixture\n" +
		"type row struct { A string; B int `json:\"b\"` }\n" +
		"type reader interface { Read() error; Close() error }\n" +
		"var rows = map[string]row{\"x\": {A: \"value\", B: 2}, \"y\": {A: \"other\", B: 3}}\n" +
		"func run() { if x := 1; x > 0 { x++; use(x) } else { use(0) }; " +
		"for i := 0; i < 2; i++ { use(i) }; switch x := 2; x { case 1, 2: use(x); use(3); default: use(4) }; " +
		"select { case value := <-input: use(value); use(5); default: use(6) } }\n"
	got := expanded(t, source)
	for _, want := range []string{
		"type row struct {\n\tA string\n\tB int",
		"type reader interface {\n\tRead() error\n\tClose() error\n}",
		"map[string]row{\n\t\"x\": {\n\t\tA: \"value\",\n\t\tB: 2,\n\t},",
		"if x := 1; x > 0 {\n\t\tx++\n\t\tuse(x)\n\t} else {\n",
		"for i := 0; i < 2; i++ {\n\t\tuse(i)\n\t}",
		"switch x := 2; x {\n\tcase 1, 2:\n\t\tuse(x)\n\t\tuse(3)\n\tdefault:\n",
		"select {\n\tcase value := <-input:\n\t\tuse(value)\n\t\tuse(5)\n\tdefault:\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expanded source omitted %q:\n%s", want, got)
		}
	}
}

func TestCommentsStringsAndNestedSyntaxArePreserved(t *testing.T) {
	source := "//go:build linux\n\npackage fixture\n" +
		"// keep declaration documentation\n" +
		"type row struct { /* field comment */ A string; B int }\n" +
		"var r = row{A: `literal; { } // not a comment`, /* value comment */ B: 2 /* tail */}\n" +
		"func run() { // body comment\n" +
		"if ok() { use(\";{}\"); /* statement comment */ use(2) } else if again() { use(3) }; " +
		"fn := func() int { defer use(4); return 5 }; use(fn()) }\n"
	got := expanded(t, source)
	for _, want := range []string{
		"//go:build linux", "// keep declaration documentation", "/* field comment */",
		"`literal; { } // not a comment`", "/* value comment */", "/* tail */",
		"// body comment", "/* statement comment */", "use(\";{}\")",
		"} else if again() {\n", "fn := func() int {\n\t\tdefer use(4)\n\t\treturn 5\n\t}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("comment, literal or nested block changed: %q\n%s", want, got)
		}
	}
}

func TestEmptyCasesAndLabeledEmptyStatementStayValid(t *testing.T) {
	source := "package fixture\nfunc run() { switch value { case 1: case 2: use(); default: }; label:; goto label }\n"
	got := expanded(t, source)
	if !strings.Contains(got, "case 1:\n\tcase 2:\n") || !strings.Contains(got, "label:") {
		t.Fatalf("empty case or labeled statement changed:\n%s", got)
	}
}

func TestExplicitSemicolonInsideControlHeadersIsUntouched(t *testing.T) {
	source := "package fixture\nfunc run() { for i := 0; i < 3; i++ { if n := i; n > 0 { use(n) } }; " +
		"switch n := f(); n { case 0: use(n) }; switch n := anyValue(); value := n.(type) { case int: use(value) } }\n"
	got := expanded(t, source)
	for _, header := range []string{
		"for i := 0; i < 3; i++ {", "if n := i; n > 0 {",
		"switch n := f(); n {", "switch n := anyValue(); value := n.(type) {",
	} {
		if !strings.Contains(got, header) {
			t.Errorf("control header changed: %s\n%s", header, got)
		}
	}
}

func TestFormattingIsIdempotentAndPreservesSyntax(t *testing.T) {
	for _, source := range []string{
		"package p; func f(){return}",
		"package p\r\nfunc f(){return}\r\n",
		"package p; func f(){}; type empty struct{}; var x=map[string]int{}",
		"package p; type x struct{A,B int; C interface{M(); N()}}",
		"package p; var x=[]struct{A int; B int}{{A:1,B:2},{A:3,B:4}}",
		"package p; var x=map[struct{A int}]struct{B int}{{A:1}:{B:2}}",
		"package p; func f(){go func(){f();f()}(); defer func(){f()}()}",
	} {
		t.Run(source, func(t *testing.T) {
			first := expanded(t, source)
			second := expanded(t, first)
			if first != second {
				t.Fatalf("formatting is not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
			}
			before, err := parser.ParseFile(token.NewFileSet(), "before.go", source, parser.ParseComments|parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			after, err := parser.ParseFile(token.NewFileSet(), "after.go", first, parser.ParseComments|parser.SkipObjectResolution)
			if err != nil {
				t.Fatal(err)
			}
			if syntaxFingerprint(before) != syntaxFingerprint(after) {
				t.Fatal("formatting changed the parsed program")
			}
		})
	}
}

func TestMalformedSourceIsRejectedWithoutOutput(t *testing.T) {
	got, err := formatSource("broken.go", []byte("package p\nfunc f( {"))
	if err == nil || got != nil {
		t.Fatalf("malformed source returned output=%q err=%v", got, err)
	}
}

func TestExistingGofmtNormalizationDoesNotTriggerSemanticGuard(t *testing.T) {
	got := expanded(t, "package p\nimport(\"strings\";\"fmt\")\nfunc f(){fmt.Println(strings.TrimSpace(\" value \"))}\n")
	if !strings.Contains(got, "\"fmt\"\n\t\"strings\"") || !strings.Contains(got, "func f() {\n") {
		t.Fatalf("imports and compact body were not normalized:\n%s", got)
	}
}

func TestNolintKeepsStatementScopeAfterLiteralExpansion(t *testing.T) {
	for _, source := range []string{
		"package p\nfunc f() (row,error) { return row{A:1,B:2},nil //nolint:nilerr // A verdict is data.\n}\n",
		"package p\nfunc f() (row,error) {\nreturn row{\nA:1,\nB:2,\n},nil //nolint:nilerr // A verdict is data.\n}\n",
	} {
		got := expanded(t, source)
		if !strings.Contains(got, "//nolint:nilerr // A verdict is data.\n\treturn row{") {
			t.Fatalf("directive stopped covering the return statement:\n%s", got)
		}
		if strings.Count(got, "//nolint:nilerr") != 1 || expanded(t, got) != got {
			t.Fatalf("directive relocation is not idempotent:\n%s", got)
		}
	}
	got := expanded(t, "package p\nfunc f() { use() //nolint:errcheck // Ignore this call.\n}\n")
	if !strings.Contains(got, "use() //nolint:errcheck // Ignore this call.") {
		t.Fatalf("a one-line statement directive was moved unnecessarily:\n%s", got)
	}
}

func TestEmptyTypesAndLiteralsStayCompact(t *testing.T) {
	source := "package p\n" +
		"type T struct{\n}\ntype I interface{\n}\n" +
		"func f() { done := make(chan struct{\n}); ptr := new(struct{\n}); " +
		"marker := struct{\n}{\n}; row := T{\n}; rows := []T{\n}; " +
		"table := map[string]struct{\n}{\n}; use(done,ptr,marker,row,rows,table) }\n"
	got := expanded(t, source)
	for _, want := range []string{
		"type T struct{}", "type I interface{}", "make(chan struct{})", "new(struct{})",
		"marker := struct{}{}", "row := T{}", "rows := []T{}", "table := map[string]struct{}{}",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("empty type or literal was expanded: %q\n%s", want, got)
		}
	}
	if expanded(t, got) != got {
		t.Fatalf("empty syntax compaction is not idempotent:\n%s", got)
	}
}

func TestCommentsInsideEmptyBracesArePreserved(t *testing.T) {
	source := "package p\n" +
		"type T struct {\n// fieldless type note\n}\n" +
		"type I interface { /* interface note */ }\n" +
		"var row = T{\n// literal note\n}\n" +
		"var table = map[string]int{ /* map note */ }\n" +
		"var marker = struct { /* inline type note */ }{ /* inline literal note */ }\n"
	got := expanded(t, source)
	for _, comment := range []string{
		"// fieldless type note", "/* interface note */", "// literal note",
		"/* map note */", "/* inline type note */", "/* inline literal note */",
	} {
		if strings.Count(got, comment) != 1 {
			t.Errorf("empty-brace comment changed: %s\n%s", comment, got)
		}
	}
	if expanded(t, got) != got {
		t.Fatalf("commented empty syntax is not idempotent:\n%s", got)
	}
}

func expanded(t *testing.T, source string) string {
	t.Helper()
	got, err := formatSource("fixture.go", []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(got, []byte("\r\n")) {
		t.Fatal("formatter did not emit canonical LF newlines")
	}
	return string(got)
}
