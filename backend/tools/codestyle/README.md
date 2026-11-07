# Expanded Go layout

From `backend/`, `go run ./tools/codestyle --check ..` checks all repository Go
files recursively, including generated code and Go fixtures outside the module.
`--fix` rewrites files and then applies `gofmt`.

The rule puts block statements, switch/select case bodies, struct/interface
field declarations and keyed literal entries on separate lines. Multiline
keyed literals receive a trailing comma. Control headers retain their required
semicolons, and `} else {` stays valid Go syntax. Empty types and literals remain
compact unless their braces contain comments.
Function input lists use one line for at most three parameters and multiline
layout for four or more. Grouped names count individually and keep their shared
type declaration. Receivers, return values and type parameters do not count.
Comments and nested types retain any newlines they require. The rule also covers
function literals, function types and interface methods.
Trailing `//nolint` directives move before a statement that expands across lines,
so their linter scope remains attached to that statement.

The formatter parses syntax before choosing edit locations and compares the
resulting syntax tree with the original after ordinary `gofmt` normalization.
Comments and string contents are never used as structural delimiters. A parse
error or changed syntax tree fails without rewriting that file. Symlinks are
not followed, and cache, vendor, node_modules and reports directories are
skipped. Saved load-test evidence and old diagnostic test assets stay intact.

`make fmt`, `fmt-check` and `lint-go` enforce the rule. Each Go generation target
formats its generated output too. The separate upstream `gofmt` rewrite rule
converts `interface{}` to `any`; the `revive` `use-any` rule enforces it, and
`predeclared` detects builtin shadowing. Generated-code hooks run that standard
rewrite before this AST layout formatter. The layout formatter does not change
type aliases or weaken its syntax-tree guard. Exit status is 0 for success, 1
for a check violation, and 2 for a usage, parsing or filesystem error.

`go run ./tools/codestyle --check-errors .` checks handwritten production error
constructors. A `fmt.Errorf` call starts with `"%s - "` and passes a visible,
nonempty local `const location` as the first substitution. Shared helpers may
accept a `location string` parameter, including inside captured closures.
Runtime `errors.New` calls use contextual `fmt.Errorf` instead; package-level
sentinel declarations remain valid. Import aliases and local shadowing are
resolved rather than matched as source text. Generated files, tests and
integration fixtures are excluded. Raw propagation is allowed, and the enabled
`errorlint` linter checks error wrapping. `make error-check` runs this check and
`lint-go` includes it; error messages are never rewritten automatically.
