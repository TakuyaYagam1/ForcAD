import click

from cli import cleanup, utils
from cli.options import with_fast_option


@click.command(help='Delete game data and stop services; preserve config and checkers')
@click.option('--full', is_flag=True, help='Also delete the local PostgreSQL cluster')
@with_fast_option
def reset(full, fast):
    cleanup.reset_game(full=full, fast=fast)
    utils.print_success('Done!')
