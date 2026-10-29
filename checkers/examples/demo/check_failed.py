#!/usr/bin/env python3
"""Demo-only checker that reports an internal checker failure."""

import sys


def main() -> int:
    if len(sys.argv) != 3 or sys.argv[1] != "check":
        print("Invalid checker call")
        return 110
    print("Checker could not complete")
    return 110


if __name__ == "__main__":
    raise SystemExit(main())
