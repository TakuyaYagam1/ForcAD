#!/usr/bin/env python3
"""Demo-only checker that reports corrupted service data."""

import sys


def main() -> int:
    if len(sys.argv) != 3 or sys.argv[1] != "check":
        print("Invalid checker call")
        return 110
    print("Stored data is corrupted")
    return 102


if __name__ == "__main__":
    raise SystemExit(main())
