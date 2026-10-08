from pydantic import BaseModel, Field
from pydantic_settings import BaseSettings, SettingsConfigDict


class Redis(BaseSettings):
    model_config = SettingsConfigDict(env_prefix='redis_')

    host: str
    port: int
    password: str
    db: int = 0

    @property
    def url(self) -> str:
        return f'redis://:{self.password}@{self.host}:{self.port}/{self.db}'


class WebCredentials(BaseSettings):
    model_config = SettingsConfigDict(env_prefix='admin_')

    username: str
    password: str


class Database(BaseSettings):
    model_config = SettingsConfigDict(env_prefix='postgres_')

    host: str
    port: int
    user: str
    password: str
    dbname: str = Field(validation_alias='postgres_db')


class Celery(BaseModel):
    broker_url: str
    result_backend: str
    timezone: str

    worker_prefetch_multiplier: int = 1
    task_acks_late: bool = True
    task_reject_on_worker_lost: bool = True
    worker_deduplicate_successful_tasks: bool = True
    # RabbitMQ 4.3 rejects transient queues shared between connections.
    control_queue_exclusive: bool = True
    event_queue_exclusive: bool = True

    result_expires: int = 15 * 60
    redis_socket_timeout: int = 10
    redis_socket_keepalive: bool = True
    redis_retry_on_timeout: bool = True

    accept_content: list[str] = ['json']
    result_accept_content: list[str] = ['json']
    result_serializer: str = 'json'
    task_serializer: str = 'json'
