from kombu.utils.json import register_type

from .attack_result import AttackResult
from .flag import Flag
from .game_config import GameConfig
from .game_state import GameState
from .task import Task
from .team import Team
from .types import Action, GameMode, TaskStatus
from .verdict import CheckerVerdict

# Only these application data types may cross the worker queue boundary.
for model in (Team, Task, Flag, CheckerVerdict):
    register_type(model, model.__name__, model.to_dict, model.from_dict)
for enum_type in (Action, TaskStatus):
    register_type(
        enum_type, enum_type.__name__, lambda value: value.value, enum_type,
    )

__all__ = (
    'Action',
    'AttackResult',
    'CheckerVerdict',
    'Flag',
    'GameConfig',
    'GameMode',
    'GameState',
    'Task',
    'TaskStatus',
    'Team',
)
