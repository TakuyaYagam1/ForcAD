"""Durable rehearsal and final sessions with separate result retention."""

from datetime import UTC, datetime
from itertools import islice

from lib import models, storage
from lib.storage import utils
from lib.storage.keys import CacheKeys


def get_session() -> dict:
    with utils.db_cursor(dict_cursor=True) as (_, curs):
        curs.execute('SELECT * FROM GameSession WHERE id=1')
        return dict(curs.fetchone())


def start(curs, real_round: int, running: bool, *, mode: str = 'auto') -> None:
    """Called with the GameConfig row locked by the organizer control."""
    curs.execute(
        'SELECT practice_start, final_start, reset_pending FROM GameSession WHERE id=1',
    )
    practice_start, final_start, reset_pending = curs.fetchone()
    if real_round or running or practice_start or final_start or reset_pending:
        raise ValueError('Game cannot be started in its current state')
    curs.execute('SELECT start_time FROM GameConfig WHERE id=1')
    scheduled, = curs.fetchone()
    now = datetime.now(UTC)
    early = scheduled is not None and now < scheduled
    if mode == 'practice' and not early:
        raise ValueError('The scheduled start has arrived; rehearsal cannot start')
    practice_start = now if mode != 'final' and early else None
    final_start = now if mode == 'final' else None
    curs.execute(
        'UPDATE GameSession SET practice_start=%s, final_start=%s, '
        'generation=generation+1 WHERE id=1', (practice_start, final_start),
    )
    curs.execute('UPDATE GameConfig SET game_running=TRUE WHERE id=1')
    utils.RedisStorage.get().delete(
        storage.game.PAUSED_AT, storage.game.PAUSED_SECONDS,
        storage.game.SCHEDULE_PAUSED_SECONDS, 'game:final_snapshot',
    )
    storage.game.set_round_start(0)


def stop(curs) -> None:
    curs.execute('UPDATE GameConfig SET game_running=FALSE WHERE id=1')
    # Cancel unpublished jobs. Sent jobs drain or expire through normal recovery.
    curs.execute(
        'UPDATE CheckerJobs SET finished_at=clock_timestamp() '
        'WHERE sent_at IS NULL AND deadline_at IS NULL AND finished_at IS NULL',
    )
    storage.game.set_game_paused(True)
    utils.RedisStorage.get().delete('game:round_waiting')


def maintain(at: datetime) -> bool:
    """End rehearsals on the wall clock, even while paused. Retry cache cleanup.

    Only rehearsal results can be erased. A finished official game is terminal.
    The SQL reset commits a pending marker before touching Redis, so a crash or
    cache outage cannot release the ticker into a half-reset game.
    """
    session = get_session()
    if session['final_start'] is not None:
        return False
    if not session['practice_start'] and not session['reset_pending']:
        return False
    with utils.db_cursor() as (conn, curs):
        curs.execute(
            'SELECT start_time, game_running FROM GameConfig WHERE id=1 FOR UPDATE',
        )
        scheduled, running = curs.fetchone()
        curs.execute(
            'SELECT practice_start, final_start, reset_pending '
            'FROM GameSession WHERE id=1',
        )
        practice_start, final_start, reset_pending = curs.fetchone()
        # Recheck under the same lock used by manual starts. A final is never
        # rehearsal data, including after it finishes or the scheduled date passes.
        if final_start is not None:
            conn.commit()
            return False
        if practice_start and (not running or at >= scheduled):
            stop(curs)
            curs.execute('SELECT 1 FROM CheckerJobs WHERE finished_at IS NULL LIMIT 1')
            if curs.fetchone():
                conn.commit()
                return False
            # Keep sequences monotonic: a delayed request must never find a new
            # official flag under an old rehearsal flag ID.
            for table in (
                'CheckerJobs', 'DispatchRuns', 'StolenFlags', 'Flags',
                'TeamTasksLog', 'ScheduleHistory',
            ):
                curs.execute(f'DELETE FROM {table}')
            curs.execute(
                'UPDATE TeamTasks SET score=t.default_score, status=-1, '
                'stolen=0, lost=0, checks=0, checks_passed=0, '
                "public_message='', private_message='', command='' "
                'FROM Tasks t WHERE t.id=TeamTasks.task_id',
            )
            curs.execute(
                'UPDATE GameConfig SET real_round=0, game_running=FALSE WHERE id=1',
            )
            curs.execute(
                'UPDATE GameSession SET practice_start=NULL, reset_pending=TRUE, '
                'generation=generation+1 WHERE id=1',
            )
            reset_pending = True
        conn.commit()
    return _finish_reset() if reset_pending else False


def _finish_reset() -> bool:
    with utils.db_cursor(dict_cursor=True) as (conn, curs):
        curs.execute('SELECT id FROM GameConfig WHERE id=1 FOR UPDATE')
        curs.execute('SELECT reset_pending FROM GameSession WHERE id=1')
        if not curs.fetchone()['reset_pending']:
            conn.commit()
            return False
        redis = utils.RedisStorage.get()
        # Preserve organizer sessions, credentials, participant configuration
        # and rate limits. Only per-game projections are disposable here.
        for pattern in (
            'flag:*', 'flags:*', 'team:*:stolen_flags', 'teamtasks:*',
            'teamtasks_history:*', 'round:*', 'scoreboard:round_snapshot:*',
        ):
            keys = redis.scan_iter(match=pattern, count=500)
            while batch := list(islice(keys, 500)):
                redis.delete(*batch)
        redis.delete(
            CacheKeys.current_round(), CacheKeys.game_state(),
            CacheKeys.attack_data(), CacheKeys.game_config(),
            storage.game.PAUSED_AT, storage.game.PAUSED_SECONDS,
            storage.game.SCHEDULE_PAUSED_SECONDS, 'game:final_snapshot',
            'game:round_waiting',
        )
        curs.execute(
            'SELECT tt.* FROM TeamTasks tt JOIN Teams t ON t.id=tt.team_id '
            'JOIN Tasks s ON s.id=tt.task_id WHERE t.active AND s.active',
        )
        state = models.GameState(
            round=0, round_start=0,
            team_tasks=storage.tasks.filter_teamtasks_for_participants(
                [dict(row) for row in curs.fetchall()],
            ),
        )
        redis.set(CacheKeys.game_state(), state.to_json())
        redis.set(CacheKeys.attack_data(), '{}')
        curs.execute('UPDATE GameSession SET reset_pending=FALSE WHERE id=1')
        conn.commit()
    return True
