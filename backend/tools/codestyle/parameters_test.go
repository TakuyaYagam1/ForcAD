package main

import (
	"strings"
	"testing"
)

func TestAtMostThreeParametersAreInline(t *testing.T) {
	for _, declaration := range []string{
		"func f(\n) {}",
		"func f(\na int,\n) {}",
		"func f(\na int,\nb string,\n) {}",
		"func f(\na int,\nb string,\nc ...bool,\n) {}",
		"func f(\na,\nb,\nc int,\n) {}",
	} {
		got := expanded(t, "package p\n"+declaration+"\n")
		if strings.Contains(got, "func f(\n") || strings.Contains(got, ",\n") {
			t.Fatalf("a short parameter list remained multiline:\n%s", got)
		}
		if expanded(t, got) != got {
			t.Fatalf("parameter compaction is not idempotent:\n%s", got)
		}
	}
}

func TestMoreThanThreeParametersAreMultiline(t *testing.T) {
	source := "package p\nfunc f(a int, b string, c bool, d ...byte) (int, error) { return 0,nil }\n"
	got := expanded(t, source)
	want := "func f(\n\ta int,\n\tb string,\n\tc bool,\n\td ...byte,\n) (int, error) {"
	if !strings.Contains(got, want) || expanded(t, got) != got {
		t.Fatalf("long parameters did not expand idempotently:\n%s", got)
	}
}

func TestParameterCountIncludesGroupedNamesButNotReceiverOrResults(t *testing.T) {
	source := "package p\n" +
		"func (r *Receiver) short(\na int,\nb int,\nc int,\n) (int,int,int,error) { return 0,0,0,nil }\n" +
		"func grouped(a,b,c,d int) {}\n" +
		"func discarded(_,_,_,_ int) {}\n" +
		"func generic[A,B,C,D any](value A) {}\n"
	got := expanded(t, source)
	for _, want := range []string{
		"func (r *Receiver) short(a int, b int, c int) (int, int, int, error)",
		"func grouped(\n\ta, b, c, d int,\n)",
		"func discarded(\n\t_, _, _, _ int,\n)",
		"func generic[A, B, C, D any](value A)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("incorrect parameter counting, missing %q:\n%s", want, got)
		}
	}
}

func TestFunctionTypesMethodsAndLiteralsUseParameterRule(t *testing.T) {
	source := "package p\n" +
		"type Callback func(a int,b int,c int,d int)\n" +
		"type Port interface { Run(a int,b int,c int,d int) error }\n" +
		"func assembly(int,string,bool,byte)\n" +
		"func factory() func(\na int,\nb int,\n) { return nil }\n" +
		"var f = func(\na int,\nb int,\n) {}\n"
	got := expanded(t, source)
	for _, want := range []string{
		"type Callback func(\n\ta int,\n\tb int,\n\tc int,\n\td int,\n)",
		"Run(\n\t\ta int,\n\t\tb int,\n\t\tc int,\n\t\td int,\n\t) error",
		"func assembly(\n\tint,\n\tstring,\n\tbool,\n\tbyte,\n)",
		"func factory() func(a int, b int)",
		"var f = func(a int, b int)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("function signature omitted %q:\n%s", want, got)
		}
	}
	if expanded(t, got) != got {
		t.Fatalf("nested signatures are not idempotent:\n%s", got)
	}
}

func TestGroupedParameterNamesHaveConsistentLayout(t *testing.T) {
	compact := expanded(t, "package p\nfunc grouped(a,b,c,d int) {}\n")
	multiline := expanded(t, "package p\nfunc grouped(\na,\nb,\nc,\nd int,\n) {}\n")
	if compact != multiline {
		t.Fatalf("grouped parameters have inconsistent layout:\n%s\n%s", compact, multiline)
	}
}

func TestParameterCommentsAndNestedTypesArePreserved(t *testing.T) {
	source := "package p\n" +
		"func comments(\n// keep parameter note\na int,\nb string,\n) {}\n" +
		"func nested(\nvalue struct { A int; B string },\ncallback func(a int,b int,c int,d int),\n) {}\n" +
		"func long(a int, /* keep inline */ b int, c int, d int /* keep tail */) {}\n"
	got := expanded(t, source)
	for _, want := range []string{
		"// keep parameter note", "/* keep inline */", "/* keep tail */",
		"func nested(value struct {\n\tA int\n\tB string\n}, callback func(\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("comment or nested type was not preserved, missing %q:\n%s", want, got)
		}
	}
	if expanded(t, got) != got {
		t.Fatalf("commented or nested signatures are not idempotent:\n%s", got)
	}
}
