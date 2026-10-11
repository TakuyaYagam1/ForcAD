import click

from forcad.cli import constants
from forcad.cli.compose import compose_command
from forcad.cli.utils import run_command


@click.command('print_tokens', help='Print team tokens')
def print_tokens():
    command = [
        *compose_command(),
        'exec', '-T', 'ticker',
        constants.RUNTIME_PYTHON, '-m', 'forcad.scripts.print_tokens',
    ]
    run_command(command, cwd=constants.BASE_DIR)
