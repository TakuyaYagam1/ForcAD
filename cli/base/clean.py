import click

from cli import cleanup, utils
from cli.options import with_fast_option


@click.command(help='Delete local game data and generated files; keep config/checkers')
@with_fast_option
def clean(fast):
    cleanup.generated_config_files()
    cleanup.reset_game(full=True, fast=fast)
    cleanup.remove_generated_config()
    utils.print_success('Cleanup successful!')
