#!/usr/bin/env python3
"""Reject compact Python suites and statement separators without dependencies."""

import argparse
import ast
from io import StringIO
from pathlib import Path
import subprocess
import sys
import tokenize


ROOT = Path(__file__).resolve().parents[1]
EXCLUDED = (".cache/", "backend/load_test/reports/", "security/semgrep/tests/")
COMPOUND = {
    "async", "case", "class", "def", "elif", "else", "except", "finally",
    "for", "if", "match", "try", "while", "with",
}


def check_source(source, filename="<source>"):
    """Use Python's parser and tokenizer so strings/annotations are not code."""
    try:
        ast.parse(source, filename=filename)
        tokens = list(tokenize.generate_tokens(StringIO(source).readline))
    except (SyntaxError, tokenize.TokenError) as error:
        return [(getattr(error, "lineno", 1) or 1, "invalid Python syntax")]
    issues = []
    first = None
    depth = 0
    for index, token in enumerate(tokens):
        if token.type == tokenize.NEWLINE:
            first = None
        if token.type in {tokenize.NL, tokenize.NEWLINE, tokenize.INDENT,
                          tokenize.DEDENT, tokenize.COMMENT, tokenize.ENDMARKER}:
            continue
        if first is None:
            first = token.string
        if token.type != tokenize.OP:
            continue
        if token.string in "([{":
            depth += 1
        elif token.string in ")]}":
            depth -= 1
        elif token.string == ";":
            issues.append((token.start[0], "put each statement on its own line; omit trailing semicolons"))
        elif token.string == ":" and depth == 0 and first in COMPOUND:
            following = tokens[index + 1]
            if following.type not in {tokenize.COMMENT, tokenize.NEWLINE}:
                issues.append((token.start[0], "put the compound statement body on following lines"))
    return issues


def tracked_python_files():
    output = subprocess.check_output(
        ["git", "-c", f"safe.directory={ROOT}", "ls-files", "--cached", "--others", "--exclude-standard", "--", "*.py"],
        cwd=ROOT, text=True,
    )
    return [ROOT / name for name in sorted(set(output.splitlines()))
            if not name.startswith(EXCLUDED) and (ROOT / name).is_file()]


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("files", nargs="*", type=Path)
    args = parser.parse_args(argv)
    failed = False
    for filename in args.files or tracked_python_files():
        issues = check_source(filename.read_text(encoding="utf-8"), str(filename))
        for line, message in issues:
            print(f"{filename}:{line}: {message}", file=sys.stderr)
            failed = True
    return int(failed)


if __name__ == "__main__":
    raise SystemExit(main())
