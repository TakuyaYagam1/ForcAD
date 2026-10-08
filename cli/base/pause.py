import click

from cli.utils import run_docker


@click.command(help='Stop updating rounds & receiving flags')
def pause():
    run_docker(['stop', 'ticker', 'http-receiver'])
    run_docker(
        [
            'exec',
            '-T',
            'client-api',
            'python',
            '/app/scripts/set_game_paused.py',
            'pause',
        ]
    )
