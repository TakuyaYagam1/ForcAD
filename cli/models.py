from datetime import datetime
from typing import List, Optional

from pydantic import BaseModel, ConfigDict, Field, field_validator


class AdminConfig(BaseModel):
    username: str
    password: str


class DatabaseConfig(BaseModel):
    user: str
    password: str
    host: str = 'postgres'
    port: int = 5432
    dbname: str = 'forcad'


class RabbitMQConfig(BaseModel):
    user: str
    password: str
    host: str = 'rabbitmq'
    port: int = 5672
    vhost: str = 'forcad'


class RedisConfig(BaseModel):
    password: str
    host: str = 'redis'
    port: int = 6379
    db: int = 0


class StoragesConfig(BaseModel):
    db: DatabaseConfig
    rabbitmq: RabbitMQConfig
    redis: RedisConfig


class GameConfig(BaseModel):
    flag_lifetime: int
    round_time: int
    rounds: Optional[int] = Field(default=None, strict=True, gt=0, le=2147483647)
    start_time: datetime

    timezone: str = 'UTC'
    default_score: float = 2500
    game_hardness: float = Field(default=10, gt=1, allow_inf_nan=False)
    mode: str = 'classic'
    get_period: Optional[int] = None
    inflation: bool = True
    volga_attacks_mode: bool = False

    checkers_path: str = '/checkers/'
    env_path: str = ''


class Task(BaseModel):
    name: str
    checker: str
    gets: int = 1
    puts: int = 1
    places: int = 1
    checker_timeout: int = 10
    checker_type: str = 'hackerdom'
    env_path: Optional[str] = None
    default_score: Optional[float] = None
    get_period: Optional[int] = None


class Team(BaseModel):
    model_config = ConfigDict(hide_input_in_errors=True)

    ip: str
    name: str
    highlighted: bool = False
    logo_path: Optional[str] = None
    token: Optional[str] = Field(
        default=None, strict=True, min_length=16, max_length=16,
        pattern=r'^[0-9a-f]{16}$', repr=False,
    )


class BasicConfig(BaseModel):
    model_config = ConfigDict(hide_input_in_errors=True)

    admin: Optional[AdminConfig] = None
    game: GameConfig
    tasks: List[Task]
    teams: List[Team]

    @field_validator('teams')
    @classmethod
    def unique_team_tokens(cls, teams: List[Team]) -> List[Team]:
        tokens = [team.token for team in teams if team.token is not None]
        if len(tokens) != len(set(tokens)):
            raise ValueError('Configured team tokens must be unique')
        return teams


class Config(BasicConfig):
    admin: AdminConfig
    storages: StoragesConfig
