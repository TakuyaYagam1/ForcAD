# Expanded Go layout

From `backend/`, check all repository Go files with:

```sh
go run ./tools/codestyle --check ..
```

This recursive check includes generated files, tests, and Go fixtures. It skips symlinks and the `.git`, `.cache`, `.codex`, `.agents`, `docker_volumes`, `node_modules`, `vendor`, and `reports` directories.

The formatter first normalizes source with `gofmt`, then expands the repository layout:

- Block statements, switch and select case bodies, struct and interface fields, and keyed literal entries use separate lines. Multiline keyed literals have a trailing comma.
- Function parameter lists stay on one line through three parameters and become multiline at four. Grouped names count separately; receivers, results, and type parameters do not count.
- Empty types and literals stay compact unless they contain comments. Required comments and nested type layouts are preserved.
- Control headers retain required semicolons, `} else {` remains valid, and `//nolint` directives stay attached to the statement they cover.

The formatter uses parsed Go syntax, not comments or string contents, to locate edits. It rejects parse errors or a changed syntax tree and does not rewrite that file. `--fix` writes changed files; inspect the result before keeping it.

The backend [Makefile](../../Makefile) uses this tool in `fmt`, `fmt-check`, and `lint-go`. From `backend/`, `make fmt` applies the expanded layout after `golangci-lint fmt`; `make fmt-check` checks both. `make lint-go` runs layout and error checks before `golangci-lint`.

`--check-errors .` checks handwritten production Go error constructors. Runtime `fmt.Errorf` messages must start with `"%s - "` and pass a visible, nonempty local `const location` as the first substitution. Helpers may accept `location string`; package-level `errors.New` sentinels remain valid. Tests, generated files, and integration fixtures are excluded. The check does not rewrite messages; `make error-check` runs it, and `lint-go` includes it.

Exit status is `0` for success, `1` for a check violation, and `2` for a usage, parse, or filesystem error.
