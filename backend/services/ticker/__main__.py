import logging
import sys
import time
from datetime import UTC, datetime, timedelta

from lib import models, storage
from lib.helpers import events
from services.tasks import get_celery_app

from . import hooks
from .hooks.utils import recover_expired_jobs
from .models import Schedule, TickerState

logger = logging.getLogger('ticker')
logger.setLevel(logging.INFO)
handler = logging.StreamHandler()
formatter = logging.Formatter('%(asctime)s - %(name)s - %(levelname)s - %(message)s')
handler.setFormatter(formatter)
logger.addHandler(handler)


def bootstrap_state() -> TickerState:
    celery_app = get_celery_app()
    already_started = storage.game.get_game_running()
    return TickerState(
        game_started=already_started,
        celery_app=celery_app,
    )


def bootstrap_schedules(state: TickerState):
    game_config = storage.game.get_current_game_config()
    session = storage.sessions.get_session()
    state.session_generation = session['generation']
    manual_start = session.get('final_start') or session['practice_start']
    start = manual_start or game_config.start_time
    start_schedule = Schedule(
        schedule_id='start_game',
        start=start,
        func=hooks.start_game,
    )
    start_schedule.load_last_run()
    state.register_schedule(start_schedule)

    round_interval = timedelta(seconds=game_config.round_time)

    if game_config.mode == models.GameMode.CLASSIC:
        rounds_schedule = Schedule(
            schedule_id='classic_rounds',
            start=start,
            func=hooks.run_classic_round,
            interval=round_interval,
        )
        rounds_schedule.load_last_run()
        state.register_schedule(rounds_schedule)

    elif game_config.mode == models.GameMode.BLITZ:
        rounds_schedule = Schedule(
            'blitz_rounds',
            start=start,
            func=hooks.run_blitz_puts_round,
            interval=round_interval,
        )
        rounds_schedule.load_last_run()
        state.register_schedule(rounds_schedule)

        sync_blitz_schedules(state)

    else:
        logger.critical('Game mode %s unsupported', game_config.mode)
        sys.exit(1)


def sync_blitz_schedules(state: TickerState):
    game_config = storage.game.get_current_game_config()
    if game_config.mode != models.GameMode.BLITZ:
        return
    prefix = 'blitz_check_gets_task_'
    session = storage.sessions.get_session()
    manual_start = session.get('final_start') or session['practice_start']
    active = {
        f'{prefix}{task.id}': task for task in storage.tasks.get_tasks()
    }
    state.schedules[:] = [
        schedule for schedule in state.schedules
        if not schedule.schedule_id.startswith(prefix) or schedule.schedule_id in active
    ]
    existing = {schedule.schedule_id: schedule for schedule in state.schedules}
    for schedule_id, task in active.items():
        interval = timedelta(seconds=task.get_period)
        if schedule_id in existing:
            existing[schedule_id].interval = interval
        else:
            schedule = Schedule(
                schedule_id,
                start=manual_start or game_config.start_time,
                func=hooks.blitz_check_gets_runner_factory(task.id),
                interval=interval,
            )
            schedule.load_last_run()
            state.register_schedule(schedule)


def sync_session(state: TickerState, at: datetime):
    reset = storage.sessions.maintain(at)
    if reset:
        events.refresh_scoreboard_after_commit()
    session = storage.sessions.get_session()
    if state.session_generation != session['generation']:
        # A failed DB read must leave the generation unchanged so the next
        # iteration retries the complete schedule rebuild.
        replacement = TickerState(state.celery_app, state.game_started)
        bootstrap_schedules(replacement)
        state.schedules = replacement.schedules
        state.session_generation = replacement.session_generation


def main(state: TickerState):
    next_maintenance = 0
    while True:
        wall_now = time.time()
        try:
            # The official deadline uses wall time, not the paused game clock.
            sync_session(state, datetime.fromtimestamp(wall_now, UTC))
        except Exception:
            logger.exception('Game session transition failed; retrying')
            time.sleep(1)
            continue
        if time.monotonic() >= next_maintenance:
            try:
                recover_expired_jobs()
                storage.game.finalize_finished_game()
            except Exception:
                logger.exception(
                    'Checker recovery failed; retrying on next maintenance',
                )
            sync_blitz_schedules(state)
            events.retry_scoreboard_refresh()
            next_maintenance = time.monotonic() + 5
        if storage.game.is_game_paused() or storage.game.is_game_finished():
            time.sleep(0.1)
            continue
        now = datetime.fromtimestamp(
            storage.game.get_scheduler_time(wall_now), UTC,
        )
        due_schedules = state.get_due_schedules(now)
        for schedule in due_schedules:
            if storage.game.is_game_paused() or storage.game.is_game_finished():
                break
            logger.info('Executing schedule %s', schedule.schedule_id)
            state.scheduled_at = now
            try:
                completed = schedule.execute(state=state)
            except Exception:
                logger.exception(
                    'Schedule %s interrupted; pending jobs will be retried',
                    schedule.schedule_id,
                )
                time.sleep(1)
                break
            if completed is False:
                # Stop BLITZ periodic dispatch while a round is draining.
                time.sleep(0.5)
                break
            logger.info('Schedule %s completed', schedule.schedule_id)
            schedule.last_run = now
            schedule.save_last_run()
        time.sleep(0.1)  # 100ms precision


if __name__ == '__main__':
    ticker_state = bootstrap_state()
    bootstrap_schedules(ticker_state)
    main(ticker_state)
