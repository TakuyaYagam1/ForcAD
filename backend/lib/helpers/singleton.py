import json
import os
from abc import ABCMeta, abstractmethod
from threading import RLock
from typing import Any, ClassVar, Generic, TypeVar

T = TypeVar('T')


class Singleton(Generic[T], metaclass=ABCMeta):
    """Generic singleton pattern implementation."""
    _values: ClassVar[dict[str, Any]] = {}
    _lock = RLock()

    @classmethod
    def __get_key(cls, data: dict[str, Any]):
        rep = json.dumps(data, sort_keys=True)
        return f'{os.getpid()}.{cls.__module__}.{cls.__name__}-{rep}'

    @staticmethod
    @abstractmethod
    def create(**kwargs) -> T:
        """This method must be overloaded to generate the instance."""

    @classmethod
    def get(cls, **kwargs) -> T:
        """This method is the getter of the instance."""
        key = cls.__get_key(kwargs)
        with cls._lock:
            if key not in cls._values:
                cls._values[key] = cls.create(**kwargs)
            return cls._values[key]


def _reset_lock():
    Singleton._lock = RLock()


os.register_at_fork(after_in_child=_reset_lock)
