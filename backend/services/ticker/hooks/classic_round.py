import logging

from celery import Celery
from celery.canvas import chain, group

from lib import models

from . import utils

logger = logging.getLogger(__name__)


def submit_full_round_jobs(
    app: Celery, team: models.Team, task: models.Task, r: int, job_id=None,
):
    kwargs, params = utils.get_round_setup(app, team, task, r, job_id)

    check = utils.get_check_signature(app, kwargs, params)
    branches = [utils.get_noop_signature(app)]
    if task.puts:
        branches.append(utils.get_puts_group(app, task, kwargs, params))
    if task.gets:
        branches.append(utils.get_gets_chain(app, task, kwargs, params))

    handler = utils.get_result_handler_signature(app, kwargs)

    scheme = chain(
        check,
        group(branches),  # Pass the CHECK result forward even without PUT/GET.
        handler,
    )
    scheme.apply_async()


def run_classic_round(state):
    return utils.dispatch_jobs(
        state, 'classic_rounds', submit_full_round_jobs, advances_round=True,
    )
