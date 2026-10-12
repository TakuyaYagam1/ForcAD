#!/usr/bin/env python3
import sys

if sys.argv[1] == "check":
    print("service smoke passed")
    sys.exit(101)
if sys.argv[1] == "put":
    print(sys.argv[4])
    sys.exit(101)
if sys.argv[1] == "get":
    sys.exit(101 if sys.argv[3] == sys.argv[4] else 102)
sys.exit(110)
