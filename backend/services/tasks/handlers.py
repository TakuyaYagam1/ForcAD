from celery import shared_task
from celery.result import AsyncResult
from celery.utils.log import get_task_logger

from lib import models, storage
from lib.helpers.jobs import JobNames
from lib.models import Action, TaskStatus

logger = get_task_logger(__name__)


@shared_task(name=JobNames.error_handler)
def exception_callback(result: AsyncResult, exc: Exception, traceback: str) -> None:
    action_name = result.task.split('.')[-1].split('_')[0].upper()
    action = Action.__members__.get(action_name, Action.CHECK)

    kw = result.kwargs
    team, task, current_round = kw['team'], kw['task'], kw['current_round']

    prev_verdict = result.args[0] if result.args else kw.get('_prev_verdict')
    if not isinstance(prev_verdict, models.CheckerVerdict):
        prev_verdict = None

    logger.error(
        f"Task exception handler was called for "
        f"team {team} task {task}, round {current_round}, "
        f"exception {exc!r}, traceback\n{traceback}"
    )

    if prev_verdict is not None and prev_verdict.status != TaskStatus.UP:
        verdict = prev_verdict
    else:
        verdict = models.CheckerVerdict(
            action=action,
            status=TaskStatus.CHECK_FAILED,
            command='',
            public_message=f'{action} failed',
            private_message=f'Exception on {action}: {exc!r}\n{traceback}',
        )

    storage.tasks.update_task_status(
        task_id=task.id,
        team_id=team.id,
        current_round=current_round,
        checker_verdict=verdict,
        job_id=kw.get('job_id'),
    )
    return verdict


@shared_task(name=JobNames.result_handler)
def checker_results_handler(
        verdicts: list[models.CheckerVerdict] | models.CheckerVerdict,
        team: models.Team,
        task: models.Task,
        current_round: int,
        job_id: str | None = None,
) -> models.CheckerVerdict:
    """
    Parse returning verdicts and return the final one.

    If there were any errors, the first error is returned
    Otherwise, verdict of the first action's verdict is returned.
    """

    # Celery passes the one-task-group's result (e.g. a group of a single put)
    # as the result itself, not the list, as documented.
    if not isinstance(verdicts, list):
        verdicts = [verdicts]

    def flatten(items):
        for item in items:
            if isinstance(item, (list, tuple)):
                yield from flatten(item)
            elif isinstance(item, models.CheckerVerdict):
                yield item

    verdicts = list(flatten(verdicts))

    check_verdict = None
    puts_verdicts = []
    gets_verdict = None
    for verdict in verdicts:
        if verdict.action == Action.CHECK:
            check_verdict = verdict
        elif verdict.action == Action.GET:
            gets_verdict = verdict
        elif verdict.action == Action.PUT:
            puts_verdicts.append(verdict)
        else:
            logger.error('Got invalid verdict action: %s', verdict.to_dict())

    logger.info(
        "Finished testing team %s task %s, round %s.\n"
        "Verdicts: check: %s; puts: %s; gets: %s.",
        team.id,
        task.id,
        current_round,
        check_verdict,
        puts_verdicts,
        gets_verdict,
    )

    parsed_verdicts = []
    if check_verdict is not None:
        parsed_verdicts.append(check_verdict)
    parsed_verdicts.extend(puts_verdicts)
    if gets_verdict is not None:
        parsed_verdicts.append(gets_verdict)

    if parsed_verdicts:
        try:
            result_verdict = next(filter(
                lambda x: x.status != TaskStatus.UP,
                parsed_verdicts,
            ))
        except StopIteration:
            result_verdict = parsed_verdicts[0]
    else:
        logger.critical('No verdicts returned from actions!')
        result_verdict = models.CheckerVerdict(
            public_message='Checker failed',
            private_message='No verdicts passed to handler',
            command='',
            status=TaskStatus.CHECK_FAILED,
            action=Action.CHECK,
        )

    storage.tasks.update_task_status(
        task_id=task.id or 0,
        team_id=team.id or 0,
        current_round=current_round,
        checker_verdict=result_verdict,
        job_id=job_id,
    )
    return result_verdict
