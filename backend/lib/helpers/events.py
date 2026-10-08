import logging
import time
import uuid

from lib import storage

logger = logging.getLogger(__name__)
PENDING_REFRESH = 'scoreboard:pending_refresh'
RECONCILE_INTERVAL = 60
_last_reconcile = 0.0


def init_scoreboard(sid=None, refresh_state=False):
    scoreboard = storage.game.construct_scoreboard(refresh_state=refresh_state)
    storage.utils.SIOManager.reliable_write_only().emit(
        'init_scoreboard',
        {'data': scoreboard},
        namespace='/game_events',
        room=sid,
    )


def refresh_scoreboard_after_commit():
    """A failed broadcast must not turn a committed CRUD request into HTTP 500."""
    token = uuid.uuid4().hex
    try:
        storage.utils.RedisStorage.get().set(PENDING_REFRESH, token)
        init_scoreboard(refresh_state=True)
        _clear_pending(token)
    except Exception:
        logger.exception('Data saved; scoreboard refresh will be retried by ticker')


def _clear_pending(token):
    storage.utils.RedisStorage.get().eval(
        "if redis.call('GET', KEYS[1]) == ARGV[1] then "
        "return redis.call('DEL', KEYS[1]) end return 0",
        1, PENDING_REFRESH, token,
    )


def retry_scoreboard_refresh():
    global _last_reconcile
    try:
        token = storage.utils.RedisStorage.get().get(PENDING_REFRESH)
        now = time.monotonic()
        reconcile = now - _last_reconcile >= RECONCILE_INTERVAL
        if token or reconcile:
            if reconcile:
                # Reload metadata even if a Redis outage lost the retry marker.
                storage.caching.flush_teams_cache()
                storage.caching.flush_tasks_cache()
            init_scoreboard(refresh_state=True)
            if token:
                _clear_pending(token)
            if reconcile:
                _last_reconcile = now
    except Exception:
        logger.exception('Scoreboard refresh unavailable; keeping pending retry')
