import itertools
import logging
import random
from datetime import UTC, datetime

from celery import Celery
from celery.canvas import chain, group, signature

from lib import models, storage
from lib.helpers.jobs import JobNames
from lib.storage import dispatch
from lib.storage.keys import CacheKeys

logger = logging.getLogger(__name__)


def get_round_processor_args(r: int, **kwargs) -> list[tuple]:
    teams = storage.teams.get_teams()
    tasks = storage.tasks.get_tasks()

    if 'task_id' in kwargs:
        tasks = list(filter(lambda task: task.id == kwargs['task_id'], tasks))

    round_args = list(itertools.product(teams, tasks, [r]))
    random.shuffle(round_args)

    return round_args


def get_noop_signature(app: Celery) -> signature:
    return app.signature(JobNames.noop_action)


def get_check_signature(app: Celery, kwargs: dict, params: dict) -> signature:
    return app.signature(JobNames.check_action, kwargs=kwargs, **params)


def get_puts_group(app: Celery, task: models.Task, kwargs: dict, params: dict) -> group:
    signatures = [
        app.signature(JobNames.put_action, kwargs=kwargs, **params)
        for _ in range(task.puts)
    ]
    return group(*signatures)


def get_gets_chain(app: Celery, task: models.Task, kwargs: dict, params: dict) -> chain:
    signatures = [
        app.signature(JobNames.get_action, kwargs=kwargs, **params)
        for _ in range(task.gets)
    ]
    return chain(*signatures)


def get_result_handler_signature(app: Celery, kwargs: dict) -> signature:
    return app.signature(JobNames.result_handler, kwargs=kwargs)


def get_round_setup(
        app: Celery,
        team: models.Team,
        task: models.Task,
        current_round: int,
        job_id=None) -> tuple[dict, dict]:
    params = {
        'time_limit': task.checker_timeout + 5,
        'link_error': app.signature(JobNames.error_handler),
    }
    kwargs = {
        'team': team,
        'task': task,
        'current_round': current_round,
    }
    if job_id is not None:
        kwargs['job_id'] = job_id
    return kwargs, params


def dispatch_jobs(state, schedule_id, submit, *, advances_round=False,
                  puts_only=False, task_id=None):
    if storage.game.is_game_paused():
        return False
    tasks = storage.tasks.get_tasks()
    if task_id is not None:
        tasks = [task for task in tasks if task.id == task_id]
    run = dispatch.prepare(
        schedule_id, storage.teams.get_teams(), tasks,
        advances_round=advances_round, puts_only=puts_only,
    )
    if run is None:
        if advances_round and not storage.game.is_game_paused():
            storage.utils.RedisStorage.get().set('game:round_waiting', 1)
        return False
    if advances_round:
        _restore_round(run)
    for job in dispatch.pending_jobs(run['id']):
        # A pause during a large batch also stops publication. The remaining
        # jobs stay in PostgreSQL and are sent after resume.
        if storage.game.is_game_paused():
            return False
        submit(state.celery_app, *dispatch.unpack_job(job), job_id=job['id'])
        dispatch.mark_sent(job['id'])
    at = state.scheduled_at or datetime.now(UTC)
    dispatch.finish(run['id'], at)
    return True


def recover_expired_jobs():
    """Release lost jobs without replaying PUTs or penalizing a team's SLA."""
    for job in dispatch.expired_jobs():
        team, task, current_round = dispatch.unpack_job(job)
        verdict = models.CheckerVerdict(
            action=models.Action.CHECK,
            status=models.TaskStatus.CHECK_FAILED,
            command='',
            public_message='Checker result unavailable',
            private_message=(
                'Checker job expired without progress. The job was closed '
                'without changing SLA counters; late results will be ignored.'
            ),
        )
        if storage.tasks.update_task_status(
            task.id, team.id, current_round, verdict, job['id'], expired_only=True,
        ):
            logger.error(
                'Recovered expired checker job %s: team=%s task=%s round=%s; '
                'SLA unchanged', job['id'], team.id, task.id, current_round,
            )


def _restore_round(run):
    new_round = run['round']
    redis = storage.utils.RedisStorage.get()
    redis.eval(
        "if redis.call('GET', KEYS[1]) ~= ARGV[1] then "
        "redis.call('SET', KEYS[1], ARGV[1]); "
        "redis.call('SET', KEYS[2], 0) end; "
        "redis.call('SET', KEYS[3], ARGV[2], 'NX'); "
        "redis.call('DEL', KEYS[4])",
        4, CacheKeys.current_round(), storage.game.PAUSED_SECONDS,
        CacheKeys.round_start(new_round), 'game:round_waiting',
        new_round, int(run['round_start']),
    )
    finished = new_round - 1
    snapshot = models.GameState(
        round=finished,
        round_start=storage.game.get_round_start(finished),
        team_tasks=storage.tasks.filter_teamtasks_for_participants(run['snapshot']),
    )
    storage.game.update_game_state(finished, snapshot)
    storage.game.update_attack_data(finished)


def update_round() -> int:
    current_round = storage.game.get_real_round_from_db()
    logger.info('Ending round %s', current_round)
    storage.game.update_round(current_round)
    logger.info('Updating game state for round %s', current_round)
    storage.game.update_game_state(current_round)

    round_to_check = current_round + 1

    if not round_to_check:
        logger.info('Not processing, round is 0')
        return 0

    logger.info('Updating attack data contents for round %s', current_round)
    storage.game.update_attack_data(current_round)

    return round_to_check
