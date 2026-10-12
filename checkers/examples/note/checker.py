#!/usr/bin/env python3
"""Executable hackerdom checker for notes with private read credentials."""

import secrets
import sys
from types import TracebackType
from typing import NoReturn

from client import Client
from protocol import PLACE_FIELDS, Arguments, Failure, State, Status

PUBLIC_HINT = False  # Set True together with checker_type: pfr in the game config.


class Checker:
    def __init__(self, host: str):
        self.client = Client(host)

    def check(self) -> None:
        state = State(secrets.token_hex(20), secrets.token_hex(16), 1)
        note = {"title": secrets.token_hex(12), "body": secrets.token_hex(24)}
        self.create(state, note)
        saved = self.read(state)
        if any(saved[field] != value for field, value in note.items()):
            raise Failure(
                Status.MUMBLE,
                "Service changes stored data",
                "Checker.check - smoke note differs after reading",
            )
        self.client.request("DELETE", state, 204)

    def put(self, identifier: str, flag: str, place: int) -> State:
        state = State(identifier, secrets.token_hex(16), place)
        note = {"title": secrets.token_hex(12), "body": secrets.token_hex(24)}
        note[PLACE_FIELDS[place]] = flag
        self.create(state, note)
        # Confirm storage before ForcAD records a successful PUT.
        self.verify(state, flag, stored=False)
        return state

    def get(self, state: State, flag: str) -> None:
        self.verify(state, flag, stored=True)

    def create(self, state: State, note: dict[str, str]) -> None:
        saved = self.client.request("PUT", state, 201, note)
        if saved.get("id") != state.note_id:
            raise Failure(
                Status.MUMBLE,
                "Invalid service response",
                "Checker.create - created note ID differs from the requested ID",
            )

    def read(self, state: State, *, stored: bool = False) -> dict:
        saved = self.client.request("GET", state, 200, stored=stored)
        if saved.get("id") != state.note_id or any(
            not isinstance(saved.get(field), str) for field in PLACE_FIELDS.values()
        ):
            raise Failure(
                Status.MUMBLE,
                "Invalid service response",
                "Checker.read - invalid note ID, title or body",
            )
        return saved

    def verify(self, state: State, flag: str, *, stored: bool) -> None:
        note = self.read(state, stored=stored)
        if note[PLACE_FIELDS[state.place]] != flag:
            raise Failure(
                Status.CORRUPT if stored else Status.MUMBLE,
                "Stored flag differs" if stored else "Service changes stored data",
                "Checker.verify - selected note field differs from the expected value",
            )


def main(args: list[str] | None = None, *, public_hint: bool = False) -> int:
    try:
        request = Arguments.parse(sys.argv[1:] if args is None else args)
        checker = Checker(request.host)
        if request.action == "put":
            state = checker.put(request.private, request.flag, request.place)
            if public_hint:
                print(state.note_id)
                print(state.encode(), file=sys.stderr)
            else:
                print(state.encode())
        else:
            if request.action == "check":
                checker.check()
            else:
                checker.get(State.decode(request.private, request.place), request.flag)
            print("OK")
        return Status.UP
    except Failure as exc:
        print(exc.public)
        print(exc.private, file=sys.stderr)
        return exc.status


def report_unexpected(
    error_type: type[BaseException],
    _error: BaseException,
    _traceback: TracebackType | None,
) -> NoReturn:
    # Exception text and traceback source lines can expose flags or credentials.
    print("Checker failed")
    print(f"checker.main - unexpected {error_type.__name__}", file=sys.stderr)
    raise SystemExit(Status.CHECK_FAILED)


def run(*, public_hint: bool = False) -> NoReturn:
    sys.excepthook = report_unexpected
    raise SystemExit(main(public_hint=public_hint))


if __name__ == "__main__":
    run(public_hint=PUBLIC_HINT)
