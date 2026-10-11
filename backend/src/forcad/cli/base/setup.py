import click
import yaml

from forcad.cli import compose, constants, models, utils
from forcad.cli.options import with_external_services_option


@click.command(help='Initialize ForcAD configuration')
@with_external_services_option
def setup(redis, database, rabbitmq, **_kwargs):
    compose.effective_compose_path()
    utils.backup_config()
    setup_runtime_directory()

    basic_config = utils.load_basic_config()
    config = utils.setup_auxiliary_structure(basic_config)
    utils.override_config(config, redis=redis, database=database, rabbitmq=rabbitmq)

    utils.dump_config(config)

    setup_db(config.storages.db)
    setup_redis(config.storages.redis)
    setup_rabbitmq(config.storages.rabbitmq)
    setup_admin_api(config.admin)

    prepare_compose(redis=redis, database=database, rabbitmq=rabbitmq)


def setup_runtime_directory():
    runtime_dir = constants.RUNTIME_DIR
    if runtime_dir.is_symlink():
        raise click.ClickException(
            f'Refusing to use a symlinked runtime directory: {runtime_dir}'
        )
    runtime_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
    if not runtime_dir.is_dir():
        raise click.ClickException(f'Runtime path is not a directory: {runtime_dir}')
    runtime_dir.chmod(0o700)


def setup_db(config: models.DatabaseConfig):
    postgres_config = [
        "# THIS FILE IS MANAGED BY 'control.py'",
        f'POSTGRES_HOST={config.host}',
        f'POSTGRES_PORT={config.port}',
        f'POSTGRES_USER={config.user}',
        f'POSTGRES_PASSWORD={config.password}',
        f'POSTGRES_DB={config.dbname}',
    ]

    utils.print_bold(f'Writing database env to {constants.POSTGRES_ENV_PATH}')
    utils.write_private_text(constants.POSTGRES_ENV_PATH, '\n'.join(postgres_config))


def setup_redis(config: models.RedisConfig):
    redis_config = [
        "# THIS FILE IS MANAGED BY 'control.py'",
        f'REDIS_HOST={config.host}',
        f'REDIS_PORT={config.port}',
        f'REDIS_PASSWORD={config.password}',
    ]

    utils.print_bold(f'Writing redis env to {constants.REDIS_ENV_PATH}')
    utils.write_private_text(constants.REDIS_ENV_PATH, '\n'.join(redis_config))


def setup_rabbitmq(config: models.RabbitMQConfig):
    management_url = f'http://{config.user}:{config.password}@{config.host}:15672/api/'
    rabbitmq_config = [
        "# THIS FILE IS MANAGED BY 'control.py'",
        f'RABBITMQ_HOST={config.host}',
        f'RABBITMQ_PORT={config.port}',
        f'RABBITMQ_DEFAULT_USER={config.user}',
        f'RABBITMQ_DEFAULT_PASS={config.password}',
        f'RABBITMQ_DEFAULT_VHOST={config.vhost}',
        f'BROKER_API_URL={management_url}',
    ]

    utils.print_bold(f'Writing broker env to {constants.RABBITMQ_ENV_PATH}')
    utils.write_private_text(constants.RABBITMQ_ENV_PATH, '\n'.join(rabbitmq_config))


def setup_admin_api(config: models.AdminConfig):
    admin_config = [
        "# THIS FILE IS MANAGED BY 'control.py'",
        f'ADMIN_USERNAME={config.username}',
        f'ADMIN_PASSWORD={config.password}',
    ]

    utils.print_bold(f'Writing admin env to {constants.ADMIN_ENV_PATH}')
    utils.write_private_text(constants.ADMIN_ENV_PATH, '\n'.join(admin_config))


def prepare_compose(redis: str, database: str, rabbitmq: str):
    if not any((redis, database, rabbitmq)):
        compose.remove_generated_compose()
        return

    try:
        with constants.FULL_COMPOSE_PATH.open(mode='r') as stream:
            base_conf = yaml.safe_load(stream)
    except OSError as error:
        raise click.ClickException(
            f'Could not read canonical compose file {constants.FULL_COMPOSE_PATH}: '
            f'{error.strerror}'
        ) from error
    if not isinstance(base_conf, dict) or not isinstance(
            base_conf.get('services'), dict):
        raise click.ClickException('Canonical compose file has no services mapping.')

    if redis:
        base_conf['services'].pop('redis', None)

    if database:
        base_conf['services'].pop('postgres', None)
        base_conf['services'].pop('postgres-data', None)

    if rabbitmq:
        base_conf['services'].pop('rabbitmq', None)

    setup_runtime_directory()
    res_path = constants.GENERATED_COMPOSE_PATH
    if res_path.exists() or res_path.is_symlink():
        compose.effective_compose_path()
    utils.print_bold(f'Writing generated compose for external storage to {res_path}')
    utils.write_private_text(
        res_path,
        f'{compose.GENERATED_COMPOSE_MARKER}\n{yaml.safe_dump(base_conf)}',
    )
