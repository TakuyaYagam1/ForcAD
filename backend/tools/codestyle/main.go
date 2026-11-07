// Command codestyle expands compact Go syntax and checks the repository rule.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, out, diagnostics io.Writer) int {
	options := flag.NewFlagSet("codestyle", flag.ContinueOnError)
	options.SetOutput(diagnostics)
	check := options.Bool("check", false, "report Go files that require expanded layout (default)")
	fix := options.Bool("fix", false, "rewrite Go files with expanded layout followed by gofmt")
	errorCheck := options.Bool("check-errors", false, "check runtime error constructors for location context")
	options.Usage = func() {
		_, _ = fmt.Fprintln(diagnostics, "Usage: codestyle [--check | --fix | --check-errors] [file-or-directory ...]")
		options.PrintDefaults()
	}
	if err := options.Parse(args); err != nil {
		return 2
	}
	if enabledModes(*check, *fix, *errorCheck) > 1 {
		_, _ = fmt.Fprintln(diagnostics, "--check, --fix and --check-errors are mutually exclusive")
		return 2
	}
	paths := options.Args()
	if len(paths) == 0 {
		paths = []string{"."}
	}
	files, err := goFiles(paths)
	if err != nil {
		_, _ = fmt.Fprintln(diagnostics, err)
		return 2
	}
	if *errorCheck {
		return runErrorCheck(files, out, diagnostics)
	}
	return runLayoutCheck(files, *fix, out, diagnostics)
}

func enabledModes(values ...bool) int {
	count := 0
	for _, enabled := range values {
		if enabled {
			count++
		}
	}
	return count
}

func runLayoutCheck(
	files []string,
	fix bool,
	out, diagnostics io.Writer,
) int {
	status := 0
	for _, path := range files {
		changed, err := processFile(path, fix)
		if err != nil {
			_, _ = fmt.Fprintf(diagnostics, "%s: %v\n", path, err)
			status = 2
			continue
		}
		if changed {
			_, _ = fmt.Fprintln(out, path)
			if !fix && status == 0 {
				status = 1
			}
		}
	}
	return status
}

func runErrorCheck(files []string, out, diagnostics io.Writer) int {
	status := 0
	for _, path := range files {
		issues, err := checkErrorFile(path)
		if err != nil {
			_, _ = fmt.Fprintf(diagnostics, "%s: %v\n", path, err)
			status = 2
			continue
		}
		for _, issue := range issues {
			_, _ = fmt.Fprintf(out, "%s: %s\n", issue.position, issue.message)
			if status == 0 {
				status = 1
			}
		}
	}
	return status
}

func processFile(path string, fix bool) (bool, error) {
	const location = "codestyle.processFile"
	source, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("%s - read source file: %w", location, err)
	}
	formatted, err := formatSource(path, source)
	if err != nil {
		return false, fmt.Errorf("%s - format source file: %w", location, err)
	}
	if bytes.Equal(source, formatted) {
		return false, nil
	}
	if fix {
		if err := replaceFile(path, formatted); err != nil {
			return false, fmt.Errorf("%s - persist formatted source: %w", location, err)
		}
	}
	return true, nil
}
