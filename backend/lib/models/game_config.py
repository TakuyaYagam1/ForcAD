import datetime
import math
from typing import Any

from dateutil.parser import parse

from .base import BaseModel


class GameConfig(BaseModel):
    """Model representing game config"""

    id: int | None
    flag_lifetime: int
    game_hardness: float
    inflation: bool
    volga_attacks_mode: bool

    round_time: int
    mode: str
    timezone: str
    start_time: datetime.datetime

    real_round: int | None
    game_running: bool | None

    table_name = 'GameConfig'

    __slots__ = (  # noqa: RUF023 - Preserve SQL and serialized field order.
        'id',
        'flag_lifetime',
        'game_hardness',
        'inflation',
        'volga_attacks_mode',
        'round_time',
        'mode',
        'timezone',
        'start_time',
        'real_round',
        'game_running',
    )

    def __init__(self, **kwargs):
        super().__init__(**kwargs)
        if not math.isfinite(self.game_hardness) or self.game_hardness <= 1:
            raise ValueError("game_hardness must be finite and greater than 1")
        if isinstance(self.start_time, str):
            self.start_time = parse(self.start_time)

    def __str__(self) -> str:
        return str(self.to_dict())

    def to_dict(self) -> dict[str, Any]:
        data = super().to_dict()
        data['start_time'] = str(data['start_time'])
        return data
