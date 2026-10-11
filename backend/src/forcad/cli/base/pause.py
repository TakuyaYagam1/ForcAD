import click

from forcad.cli import constants
from forcad.cli.utils import run_docker


@click.command(help='Stop updating rounds & receiving flags')
def pause():
    run_docker(
        [
            'exec',
            '-T',
            'client-api',
            constants.RUNTIME_PYTHON,
            '-m',
            'forcad.scripts.set_game_paused',
            'pause',
        ]
    )
