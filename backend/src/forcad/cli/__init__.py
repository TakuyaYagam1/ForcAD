import click

from . import base


@click.group()
def cli():
    pass


base.register(cli)


def main():
    cli()


__all__ = ('cli', 'main')
