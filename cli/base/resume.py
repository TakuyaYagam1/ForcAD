import click

from cli.utils import run_docker


@click.command(
    help='Resume the game after pause',
)
def resume():
    run_docker(
        [
            'exec',
            '-T',
            'client-api',
            'python',
            '/app/scripts/set_game_paused.py',
            'resume',
        ]
    )
    run_docker(['start', 'ticker', 'http-receiver'])
