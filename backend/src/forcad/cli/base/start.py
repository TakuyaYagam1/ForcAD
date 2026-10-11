import click

from forcad.cli import constants
from forcad.cli.options import with_fast_option, with_workers_option
from forcad.cli.utils import print_success, run_docker


@click.command(help='Start ForcAD, building if necessary')
@with_fast_option
@with_workers_option
def start(workers, **_kwargs):
    print_success(f'Using Forcad:{constants.VERSION}')
    run_docker(['up', '-d', '--scale', f'celery={workers}'])
