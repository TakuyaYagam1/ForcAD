import secrets
from typing import Any, ClassVar

from forcad.lib.team_logos import safe_logo_path

from .base import BaseModel


class Team(BaseModel):
    """Model representing a team."""

    id: int | None
    name: str
    ip: str
    token: str
    highlighted: bool
    active: bool
    logo_path: str

    table_name = 'Teams'

    defaults: ClassVar[dict[str, Any]] = {
        'highlighted': False,
        'active': True,
        'logo_path': '',
    }

    __slots__ = (  # noqa: RUF023 - Preserve SQL and serialized field order.
        'id',
        'name',
        'ip',
        'token',
        'highlighted',
        'active',
        'logo_path',
    )

    def __init__(self, **kwargs: Any):
        super().__init__(**kwargs)
        self.logo_path = safe_logo_path(self.logo_path)

    @staticmethod
    def generate_token() -> str:
        return secrets.token_hex(8)

    def to_dict_for_participants(self) -> dict[str, Any]:
        d = self.to_dict()
        d.pop('token', None)
        return d

    def __str__(self) -> str:
        return f"Team({self.id}, {self.name})"
