from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import datetime, timedelta

from celery import Celery

from lib.storage import schedules


@dataclass
class Schedule:
    schedule_id: str
    start: datetime
    func: Callable
    end: datetime | None = None
    last_run: datetime | None = None
    interval: timedelta | None = None

    def execute(self, state: 'TickerState'):
        return self.func(state=state)

    def load_last_run(self) -> None:
        self.last_run = schedules.get_last_run(self.schedule_id)

    def save_last_run(self) -> None:
        schedules.set_last_run(self.schedule_id, self.last_run)

    def is_expired(self, at: datetime) -> bool:
        if self.end is not None and at >= self.end:
            return True
        if self.interval is None and self.last_run is not None:
            return True
        return False

    def should_be_called(self, at: datetime) -> bool:
        if self.is_expired(at):
            return False

        if at < self.start:
            return False

        if self.last_run is None:
            return True

        if self.interval is not None and self.last_run + self.interval <= at:
            return True

        return False


@dataclass
class TickerState:
    celery_app: Celery
    game_started: bool
    schedules: list[Schedule] = field(default_factory=list)
    scheduled_at: datetime | None = None

    def register_schedule(self, schedule: Schedule) -> None:
        self.schedules.append(schedule)

    def get_due_schedules(self, at: datetime) -> list[Schedule]:
        return list(filter(
            lambda x: x.should_be_called(at),
            self.schedules,
        ))
