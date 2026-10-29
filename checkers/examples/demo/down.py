#!/usr/bin/env python3
"""Demo-only checker for an unavailable service."""

import sys


def main() -> int:
    if len(sys.argv) != 3 or sys.argv[1] != "check":
        print("Invalid checker call")
        return 110
    print("Connection refused")
    return 104


if __name__ == "__main__":
    raise SystemExit(main())
