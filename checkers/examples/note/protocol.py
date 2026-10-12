"""ForcAD arguments, verdicts and private retrieval data."""

import json
import re
from dataclasses import dataclass
from enum import IntEnum

NOTE_ID = re.compile(r"[0-9a-f]{40}")
TOKEN = re.compile(r"[0-9a-f]{32}")
FLAG = re.compile(r"[A-Z0-9]{31}=")
STATE_VERSION = 1
MAX_STATE_BYTES = 4096
PLACE_FIELDS = {1: "title", 2: "body"}


class Status(IntEnum):
    UP = 101
    CORRUPT = 102
    MUMBLE = 103
    DOWN = 104
    CHECK_FAILED = 110


class Failure(Exception):
    def __init__(self, status: Status, public: str, private: str):
        super().__init__(private)
        self.status = status
        self.public = public
        self.private = private


@dataclass(frozen=True)
class Arguments:
    action: str
    host: str
    private: str = ""
    flag: str = ""
    place: int = 1

    @classmethod
    def parse(cls, args: list[str]) -> "Arguments":
        if len(args) == 2 and args[0] == "check":
            return cls(*args)
        if len(args) != 5 or args[0] not in {"put", "get"}:
            raise Failure(
                Status.CHECK_FAILED,
                "Checker invocation failed",
                "Arguments.parse - expected check HOST or "
                "put/get HOST PRIVATE FLAG PLACE",
            )
        action, host, private, flag, raw_place = args
        if raw_place not in {
            str(place) for place in PLACE_FIELDS
        } or not FLAG.fullmatch(flag):
            raise Failure(
                Status.CHECK_FAILED,
                "Checker invocation failed",
                "Arguments.parse - invalid flag format or unsupported place",
            )
        if action == "put" and not NOTE_ID.fullmatch(private):
            raise Failure(
                Status.CHECK_FAILED,
                "Checker invocation failed",
                "Arguments.parse - PUT ID must contain 40 lowercase hex characters",
            )
        return cls(action, host, private, flag, int(raw_place))


@dataclass(frozen=True)
class State:
    note_id: str
    token: str
    place: int

    def encode(self) -> str:
        return json.dumps(
            {
                "version": STATE_VERSION,
                "id": self.note_id,
                "token": self.token,
                "place": self.place,
            },
            separators=(",", ":"),
        )

    @classmethod
    def decode(cls, raw: str, place: int) -> "State":
        try:
            if len(raw.encode()) > MAX_STATE_BYTES:
                raise ValueError("state too large")
            data = json.loads(raw)
            if (
                not isinstance(data, dict)
                or type(data.get("version")) is not int
                or data["version"] != STATE_VERSION
                or type(data.get("place")) is not int
                or data["place"] != place
                or not isinstance(data.get("id"), str)
                or not NOTE_ID.fullmatch(data["id"])
                or not isinstance(data.get("token"), str)
                or not TOKEN.fullmatch(data["token"])
            ):
                raise ValueError("invalid state")
        except (ValueError, RecursionError) as exc:
            raise Failure(
                Status.CHECK_FAILED,
                "Checker state is invalid",
                "State.decode - invalid retrieval data or place mismatch",
            ) from exc
        return cls(data["id"], data["token"], place)
