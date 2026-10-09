"""Durable checker dispatch and an accounting barrier between game rounds."""

import time
import uuid
from datetime import datetime

from psycopg2.extras import Json

from lib import models, storage
from lib.storage import utils

# Allow queueing and callback delivery beyond an individual checker's timeout.
RECOVERY_GRACE_SECONDS = 300


def prepare(schedule_id, teams, tasks, *, advances_round=False, puts_only=False):
    """Resume an unfinished dispatch, or atomically plan its entire next batch.

    Publication uses at-least-once delivery. A job's stable ID deduplicates its
    accounting transaction even if the broker's acknowledgement was lost.
    """
    with utils.db_cursor(dict_cursor=True) as (conn, curs):
        curs.execute(
            'SELECT real_round, game_running, rounds '
            'FROM GameConfig WHERE id=1 FOR UPDATE',
        )
        game = curs.fetchone()
        curs.execute('SELECT reset_pending FROM GameSession WHERE id=1')
        resetting = curs.fetchone()['reset_pending']
        current_round = game['real_round']
        if not game['game_running'] or resetting or storage.game.is_game_paused():
            conn.commit()
            return None
        curs.execute(
            'SELECT * FROM DispatchRuns WHERE schedule_id=%s AND NOT completed',
            (schedule_id,),
        )
        pending = curs.fetchone()
        if pending:
            conn.commit()
            return dict(pending)

        if advances_round:
            # This hook runs at the next round boundary, not when the final
            # round begins. Resume any interrupted publication above first.
            # Finish before the accounting barrier: sent checks may drain, but
            # must not extend flag reception beyond the last playable round.
            if game['rounds'] is not None and current_round >= game['rounds']:
                storage.sessions.stop(curs)
                conn.commit()
                return None
            curs.execute(
                'SELECT 1 FROM CheckerJobs '
                'WHERE round <= %s AND finished_at IS NULL LIMIT 1',
                (current_round,),
            )
            if curs.fetchone():
                conn.commit()
                return None
        elif current_round < 1:
            conn.commit()
            return None

        new_round = current_round + int(advances_round)
        run_id = uuid.uuid4().hex
        started = time.time()
        snapshot = None
        if advances_round:
            curs.execute(
                'SELECT tt.* FROM TeamTasks tt '
                'JOIN Teams t ON t.id=tt.team_id '
                'JOIN Tasks s ON s.id=tt.task_id WHERE t.active AND s.active',
            )
            snapshot = [dict(cell) for cell in curs.fetchall()]
            curs.execute(
                'UPDATE GameConfig SET real_round=%s WHERE id=1', (new_round,),
            )

        curs.execute(
            'INSERT INTO DispatchRuns '
            '(id, schedule_id, round, is_round, round_start, snapshot) '
            'VALUES (%s,%s,%s,%s,%s,%s) RETURNING *',
            (run_id, schedule_id, new_round, advances_round, started,
             Json(snapshot)),
        )
        run = dict(curs.fetchone())
        for team in teams:
            for task in tasks:
                if puts_only and task.puts == 0:
                    continue
                payload = {'team': team.to_dict(), 'task': task.to_dict()}
                curs.execute(
                    'INSERT INTO CheckerJobs (id, run_id, round, payload) '
                    'VALUES (%s,%s,%s,%s)',
                    (uuid.uuid4().hex, run_id, new_round, Json(payload)),
                )
        conn.commit()
        return run


def pending_jobs(run_id):
    with utils.db_cursor(dict_cursor=True) as (_, curs):
        curs.execute(
            'SELECT * FROM CheckerJobs WHERE run_id=%s AND sent_at IS NULL '
            'AND finished_at IS NULL ORDER BY id', (run_id,),
        )
        return [dict(row) for row in curs.fetchall()]


def mark_sent(job_id):
    with utils.db_cursor() as (conn, curs):
        curs.execute(
            'UPDATE CheckerJobs SET sent_at=now(), '
            'deadline_at=COALESCE(deadline_at, clock_timestamp() + make_interval('
            "secs => (payload->'task'->>'checker_timeout')::integer + %s)) "
            'WHERE id=%s', (RECOVERY_GRACE_SECONDS, job_id),
        )
        conn.commit()


def touch_job(job_id):
    """Renew a live job before an action; fence already completed jobs."""
    if job_id is None:
        return True
    with utils.db_cursor() as (conn, curs):
        curs.execute(
            'UPDATE CheckerJobs SET deadline_at=clock_timestamp() + make_interval('
            "secs => (payload->'task'->>'checker_timeout')::integer + %s) "
            'WHERE id=%s AND finished_at IS NULL RETURNING id',
            (RECOVERY_GRACE_SECONDS, job_id),
        )
        active = curs.fetchone() is not None
        conn.commit()
        return active


def expired_jobs():
    """Read bounded candidates; claim_result rechecks the lease atomically."""
    with utils.db_cursor(dict_cursor=True) as (_, curs):
        curs.execute(
            'SELECT * FROM CheckerJobs WHERE finished_at IS NULL '
            'AND deadline_at <= clock_timestamp() ORDER BY deadline_at, id LIMIT 100',
        )
        return [dict(row) for row in curs.fetchall()]


def finish(run_id, at: datetime):
    """Commit the schedule checkpoint with its completed dispatch."""
    with utils.db_cursor() as (conn, curs):
        curs.execute(
            'UPDATE DispatchRuns SET completed=TRUE WHERE id=%s '
            'AND NOT EXISTS (SELECT 1 FROM CheckerJobs '
            'WHERE run_id=%s AND sent_at IS NULL AND finished_at IS NULL) '
            'RETURNING schedule_id', (run_id, run_id),
        )
        result = curs.fetchone()
        if result is None:
            raise RuntimeError('Cannot finish a dispatch with unpublished jobs')
        schedule_id, = result
        curs.execute(
            'INSERT INTO ScheduleHistory(id,last_run) VALUES (%s,%s) '
            'ON CONFLICT(id) DO UPDATE SET last_run=EXCLUDED.last_run',
            (schedule_id, at),
        )
        conn.commit()


def claim_result(curs, job_id, *, expired_only=False):
    """Called in the same transaction as the SLA update."""
    if job_id is None:
        if expired_only:
            raise ValueError('Recovery requires a checker job ID')
        return True
    curs.execute(
        'UPDATE CheckerJobs SET finished_at=now() '
        'WHERE id=%s AND finished_at IS NULL '
        'AND (NOT %s OR deadline_at <= clock_timestamp()) RETURNING id',
        (job_id, expired_only),
    )
    return curs.fetchone() is not None


def unpack_job(job):
    payload = job['payload']
    return (
        models.Team.from_dict(payload['team']),
        models.Task.from_dict(payload['task']),
        job['round'],
    )
