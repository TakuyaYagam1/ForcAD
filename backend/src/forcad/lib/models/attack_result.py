from typing import Any, ClassVar

from .base import BaseModel


class AttackResult(BaseModel):
    attacker_id: int
    victim_id: int
    task_id: int
    submit_ok: bool
    message: str
    attacker_delta: float
    victim_delta: float
    generation: int

    __slots__ = (  # noqa: RUF023 - Preserve SQL and serialized field order.
        'attacker_id',
        'victim_id',
        'task_id',
        'submit_ok',
        'message',
        'attacker_delta',
        'victim_delta',
        'generation',
    )

    defaults: ClassVar[dict[str, Any]] = {
        'victim_id': 0,
        'task_id': 0,
        'submit_ok': False,
        'message': '',
        'attacker_delta': 0.0,
        'victim_delta': 0.0,
        'generation': 0,
    }

    labels = ('attacker_id', 'victim_id', 'task_id', 'submit_ok')

    def get_label_key(self) -> tuple[Any, ...]:
        return tuple(getattr(self, k) for k in self.labels)

    def get_label_values(self) -> dict[str, Any]:
        return {k: getattr(self, k) for k in self.labels}

    def get_flag_notification(self) -> dict[str, Any]:
        return {
            'attacker_id': self.attacker_id,
            'victim_id': self.victim_id,
            'task_id': self.task_id,
            'attacker_delta': self.attacker_delta,
            'generation': self.generation,
        }
