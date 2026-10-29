#!/usr/bin/env python3
"""Demo-only checker for a service with an invalid response."""

import sys


def main() -> int:
    if len(sys.argv) != 3 or sys.argv[1] != "check":
        print("Invalid checker call")
        return 110
    print("Invalid response")
    return 103


if __name__ == "__main__":
    raise SystemExit(main())
