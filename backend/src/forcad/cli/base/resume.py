import click

from forcad.cli import constants
from forcad.cli.utils import run_docker


@click.command(
    help='Resume the game after pause',
)
def resume():
    run_docker(
        [
            'exec',
            '-T',
            'client-api',
            constants.RUNTIME_PYTHON,
            '-m',
            'forcad.scripts.set_game_paused',
            'resume',
        ]
    )
