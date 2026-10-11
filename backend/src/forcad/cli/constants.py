from forcad.paths import resolve_project_root

ADMIN_USER = 'forcad'

BASE_DIR = resolve_project_root()
DEPLOYMENT_DIR = BASE_DIR / 'deployment' / 'docker'
FULL_COMPOSE_PATH = DEPLOYMENT_DIR / 'compose.yml'
TESTS_COMPOSE_PATH = DEPLOYMENT_DIR / 'compose.tests.yml'
GENERATED_COMPOSE_PATH = BASE_DIR / 'config' / 'runtime' / 'compose.external.yml'
RUNTIME_DIR = BASE_DIR / 'config' / 'runtime'
CONFIG_PATH = BASE_DIR / 'config' / 'config.yml'
CONFIG_EXAMPLE_PATH = BASE_DIR / 'config' / 'config.yml.example'
VERSION_PATH = BASE_DIR / '.version'

ADMIN_ENV_PATH = RUNTIME_DIR / 'admin.env'
POSTGRES_ENV_PATH = RUNTIME_DIR / 'postgres_environment.env'
RABBITMQ_ENV_PATH = RUNTIME_DIR / 'rabbitmq_environment.env'
REDIS_ENV_PATH = RUNTIME_DIR / 'redis_environment.env'

RUNTIME_PYTHON = '/opt/forcad/bin/python'

try:
    VERSION = VERSION_PATH.read_text().strip()
except FileNotFoundError:
    VERSION = 'latest'
