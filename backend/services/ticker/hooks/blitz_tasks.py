import logging
from collections.abc import Callable
from copy import deepcopy

from celery import Celery
from celery.canvas import chain, group

from lib import models

from . import utils

logger = logging.getLogger(__name__)


def submit_puts_jobs(
    app: Celery, team: models.Team, task: models.Task, r: int, job_id=None,
):
    if task.puts == 0:
        return
    kwargs, params = utils.get_round_setup(app, team, task, r, job_id)

    handler = utils.get_result_handler_signature(app, kwargs)

    put_kwargs = deepcopy(kwargs)
    put_kwargs['_prev_verdict'] = None
    puts = utils.get_puts_group(app, task, put_kwargs, params)
    scheme = chain(puts, handler)
    scheme.apply_async()


def submit_check_gets_jobs(
    app: Celery, team: models.Team, task: models.Task, r: int, job_id=None,
):
    kwargs, params = utils.get_round_setup(app, team, task, r, job_id)

    handler = utils.get_result_handler_signature(app, kwargs)
    check = utils.get_check_signature(app, kwargs, params)
    branches = [utils.get_noop_signature(app)]
    if task.gets:
        branches.append(utils.get_gets_chain(app, task, kwargs, params))
    scheme = chain(check, group(branches), handler)
    scheme.apply_async()


def run_blitz_puts_round(state):
    return utils.dispatch_jobs(
        state, 'blitz_rounds', submit_puts_jobs,
        advances_round=True, puts_only=True,
    )


def blitz_check_gets_runner_factory(task_id: int) -> Callable:
    def run_blitz_check_gets_round(state):
        return utils.dispatch_jobs(
            state, f'blitz_check_gets_task_{task_id}', submit_check_gets_jobs,
            task_id=task_id,
        )

    return run_blitz_check_gets_round
