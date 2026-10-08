import time

from kombu.utils import json as kjson
from redis import WatchError

from lib import models, storage
from lib.helpers.cache import cache_helper
from lib.storage import caching, utils
from lib.storage.keys import CacheKeys

_CURRENT_REAL_ROUND_QUERY = 'SELECT real_round FROM GameConfig WHERE id=1'

_UPDATE_REAL_ROUND_QUERY = 'UPDATE GameConfig SET real_round = %(round)s WHERE id=1'

_SET_GAME_RUNNING_QUERY = (
    'UPDATE GameConfig SET game_running = %(value)s WHERE id=1 '
    'AND game_running IS DISTINCT FROM %(value)s '
    'AND (NOT %(value)s OR real_round=0)'
)

_GET_GAME_RUNNING_QUERY = 'SELECT game_running FROM GameConfig WHERE id=1'

_GET_GAME_CONFIG_QUERY = 'SELECT * FROM GameConfig WHERE id=1'


def get_round_start(r: int) -> int:
    """Get start time for round as unix timestamp."""
    with utils.redis_pipeline(transaction=False) as pipe:
        (start_time,) = pipe.get(CacheKeys.round_start(r)).execute()
    return int(start_time or 0)


def set_round_start(r: int) -> None:
    """Set start time for round as str."""
    cur_time = int(time.time())
    with utils.redis_pipeline(transaction=False) as pipe:
        pipe.set(CacheKeys.round_start(r), cur_time).execute()


def get_real_round() -> int:
    """
    Get real round of system (for flag submitting).

    :returns: -1 if round not in cache, else round
    """
    with utils.redis_pipeline(transaction=False) as pipe:
        (r,) = pipe.get(CacheKeys.current_round()).execute()

    return int(r or -1)


def get_real_round_from_db() -> int:
    """Get real round from database. Fully persistent to use with game management."""
    with utils.db_cursor() as (_, curs):
        curs.execute(_CURRENT_REAL_ROUND_QUERY)
        (r,) = curs.fetchone()

    return r


def update_real_round_in_db(new_round: int) -> None:
    """Update real_round of game config stored in DB."""
    with utils.db_cursor() as (conn, curs):
        curs.execute(_UPDATE_REAL_ROUND_QUERY, {'round': new_round})
        conn.commit()


def set_game_running(new_value: bool) -> bool:
    """Start only a new game; a finished game must never restart."""
    with utils.db_cursor() as (conn, curs):
        curs.execute(_SET_GAME_RUNNING_QUERY, {'value': new_value})
        changed = curs.rowcount == 1
        conn.commit()
        return changed


def get_game_running() -> bool:
    """Get current game_running value from db."""
    with utils.db_cursor() as (_, curs):
        curs.execute(_GET_GAME_RUNNING_QUERY)
        (game_running,) = curs.fetchone()

    return game_running


def get_db_game_config() -> models.GameConfig:
    """Get game config from database."""
    with utils.db_cursor(dict_cursor=True) as (_, curs):
        curs.execute(_GET_GAME_CONFIG_QUERY)
        result = curs.fetchone()

    return models.GameConfig.from_dict(result)


def get_current_game_config() -> models.GameConfig:
    """Get game config from cache is cached, cache it otherwise."""
    with utils.redis_pipeline(transaction=True) as pipe:
        cache_helper(
            pipeline=pipe,
            cache_key=CacheKeys.game_config(),
            cache_func=caching.cache_game_config,
            cache_args=(pipe,),
        )

        (result,) = pipe.get(CacheKeys.game_config()).execute()

    game_config = models.GameConfig.from_json(result)
    return game_config


def construct_game_state_from_db(current_round: int) -> models.GameState:
    """Get game state for specified round with teamtasks from db."""
    teamtasks = storage.tasks.get_teamtasks_from_db()
    teamtasks = storage.tasks.filter_teamtasks_for_participants(teamtasks)

    team_ids = {team.id for team in storage.teams.get_teams()}
    task_ids = {task.id for task in storage.tasks.get_tasks()}

    teamtasks = list(
        filter(
            lambda tt: tt['team_id'] in team_ids and tt['task_id'] in task_ids,
            teamtasks,
        )
    )

    round_start = get_round_start(current_round)
    state = models.GameState(
        round_start=round_start,
        round=current_round,
        team_tasks=teamtasks,
    )
    return state


def construct_latest_game_state(current_round: int) -> models.GameState:
    """Get game state from latest teamtasks from redis stream."""
    teamtasks = storage.tasks.get_last_teamtasks()
    teamtasks = storage.tasks.filter_teamtasks_for_participants(teamtasks)

    round_start = get_round_start(current_round)
    state = models.GameState(
        round_start=round_start,
        round=current_round,
        team_tasks=teamtasks,
    )
    return state


def get_cached_game_state() -> models.GameState | None:
    with storage.utils.redis_pipeline(transaction=False) as pipe:
        (state,) = pipe.get(CacheKeys.game_state()).execute()

    if not state:
        return None
    return models.GameState.from_json(state)


def refresh_cached_game_state(force: bool = True) -> models.GameState:
    """Merge new active pairs without overwriting a concurrently completed round."""
    with utils.redis_pipeline(transaction=True) as pipe:
        while True:
            try:
                pipe.watch(CacheKeys.game_state())
                cached = pipe.get(CacheKeys.game_state())
                state = models.GameState.from_json(cached) if cached else None
                if state is not None and not force:
                    pipe.unwatch()
                    return state
                current_round = state.round if state else max(0, get_real_round() - 1)
                fresh = construct_game_state_from_db(current_round)
                previous = (
                    {
                        (cell['team_id'], cell['task_id']): cell
                        for cell in state.team_tasks
                    }
                    if state
                    else {}
                )
                # Preserve completed-round results; DB may already contain
                # in-flight checks.
                fresh.team_tasks = [
                    previous.get((cell['team_id'], cell['task_id']), cell)
                    for cell in fresh.team_tasks
                ]
                pipe.multi()
                pipe.set(CacheKeys.game_state(), fresh.to_json())
                pipe.execute()
                return fresh
            except WatchError:
                continue


def construct_scoreboard(refresh_state: bool = False) -> dict:
    """
    Get formatted scoreboard to serve to frontend.
    Fetches and constructs the full scoreboard (state, teams, tasks, config).
    """

    teams = [team.to_dict_for_participants() for team in storage.teams.get_teams()]
    tasks = [task.to_dict_for_participants() for task in storage.tasks.get_tasks()]
    cfg = storage.game.get_current_game_config().to_dict()

    state = get_cached_game_state()
    if refresh_state or state is None:
        state = refresh_cached_game_state(force=refresh_state)
    state = state.to_dict()

    data = {
        'state': state,
        'teams': teams,
        'tasks': tasks,
        'config': cfg,
        'runtime': get_runtime_status(),
    }

    return data


def construct_ctftime_scoreboard() -> list | None:
    game_state = get_cached_game_state()

    if not game_state:
        return None

    teams = storage.teams.get_teams()

    standings = []
    for team in teams:
        team_id = team.id
        teamtasks = list(
            filter(
                lambda x: x['team_id'] == team_id,
                game_state.team_tasks,
            )
        )

        score = sum(
            map(
                lambda x: (
                    x['score'] * x['checks_passed'] / x['checks']
                    if x['checks'] > 0
                    else 0
                ),
                teamtasks,
            )
        )
        standings.append({'team': team.name, 'score': score, 'id': team.id})

    standings = sorted(standings, key=lambda x: (-x['score'], x['id']))
    standings = [
        {'pos': i + 1, 'team': data['team'], 'score': round(data['score'], 2)}
        for i, data in enumerate(standings)
    ]
    return standings


def update_round(finished_round: int) -> None:
    new_round = finished_round + 1

    set_round_start(r=new_round)
    update_real_round_in_db(new_round=new_round)

    with utils.redis_pipeline(transaction=False) as pipe:
        pipe.set(CacheKeys.current_round(), new_round)
        pipe.set("game:paused_seconds", 0)
        pipe.execute()


def update_attack_data(current_round: int) -> None:
    tasks = storage.tasks.get_tasks()
    tasks = list(filter(lambda x: x.checker_provides_public_flag_data, tasks))
    attack_data = storage.flags.get_attack_data(current_round, tasks)
    with utils.redis_pipeline(transaction=False) as pipe:
        pipe.set(CacheKeys.attack_data(), kjson.dumps(attack_data))
        pipe.execute()


def update_game_state(
    for_round: int, game_state: models.GameState | None = None,
) -> models.GameState:
    if game_state is None:
        game_state = storage.game.construct_game_state_from_db(for_round)
    marker = f'scoreboard:round_snapshot:{for_round}'
    with utils.redis_pipeline(transaction=True) as pipe:
        while True:
            try:
                pipe.watch(marker)
                recorded = pipe.exists(marker)
                pipe.multi()
                pipe.set(CacheKeys.game_state(), game_state.to_json())
                if for_round > 0 and not recorded:
                    for cell in game_state.team_tasks:
                        pipe.xadd(
                            CacheKeys.teamtasks_history(
                                cell['team_id'], cell['task_id'],
                            ),
                            {**cell, 'round': for_round},
                            maxlen=50,
                            approximate=False,
                        )
                    pipe.set(marker, 1)
                pipe.execute()
                break
            except WatchError:
                continue

    utils.SIOManager.write_only().emit(
        event='update_scoreboard',
        data={'data': game_state.to_dict()},
        namespace='/game_events',
    )

    return game_state


PAUSED_AT = 'game:paused_at'
PAUSED_SECONDS = 'game:paused_seconds'
SCHEDULE_PAUSED_SECONDS = 'game:schedule_paused_seconds'


def set_game_paused(paused: bool) -> None:
    redis = utils.RedisStorage.get()
    now = time.time()
    # Lua makes repeated pause/resume calls atomic and idempotent.
    redis.eval(
        """
        if ARGV[1] == '1' then
            redis.call('SET', KEYS[1], ARGV[2], 'NX')
        else
            local started = redis.call('GET', KEYS[1])
            if started then
                redis.call('INCRBYFLOAT', KEYS[2],
                    math.max(0, tonumber(ARGV[2]) - tonumber(started)))
                redis.call('INCRBYFLOAT', KEYS[3],
                    math.max(0, tonumber(ARGV[2]) - tonumber(started)))
                redis.call('DEL', KEYS[1])
            end
        end
    """,
        3,
        PAUSED_AT,
        PAUSED_SECONDS,
        SCHEDULE_PAUSED_SECONDS,
        '1' if paused else '0',
        now,
    )


def get_scheduler_time(at: float) -> float:
    """Logical ticker clock: completed and ongoing pauses do not advance it.

    Unlike the per-round pause counter, the schedule offset survives new rounds
    and ticker restarts. ScheduleHistory timestamps use this same clock.
    """
    paused_at, total = utils.RedisStorage.get().mget(
        PAUSED_AT, SCHEDULE_PAUSED_SECONDS,
    )
    ongoing = max(0, at - float(paused_at)) if paused_at else 0
    return at - float(total or 0) - ongoing


def is_game_paused() -> bool:
    return bool(utils.RedisStorage.get().exists(PAUSED_AT))


def get_lifecycle() -> tuple[int, bool]:
    with utils.db_cursor() as (_, curs):
        curs.execute('SELECT real_round, game_running FROM GameConfig WHERE id=1')
        row = curs.fetchone()
    if row is None:
        raise ValueError('Game is not initialized')
    return row


def is_game_finished() -> bool:
    real_round, running = get_lifecycle()
    return real_round > 0 and not running


def has_pending_checks() -> bool:
    with utils.db_cursor() as (_, curs):
        curs.execute(
            'SELECT EXISTS (SELECT 1 FROM CheckerJobs WHERE finished_at IS NULL)',
        )
        return curs.fetchone()[0]


def change_game_state(action: str) -> None:
    """Serialize organizer controls with round creation and flag accounting."""
    if action not in {'pause', 'resume', 'finish'}:
        raise ValueError('Unknown game action')
    with utils.db_cursor() as (conn, curs):
        curs.execute(
            'SELECT real_round, game_running FROM GameConfig WHERE id=1 FOR UPDATE',
        )
        row = curs.fetchone()
        if row is None:
            raise ValueError('Game is not initialized')
        real_round, running = row
        if real_round > 0 and not running:
            if action == 'finish':
                conn.commit()
                return
            raise ValueError('Game has already finished')
        if action == 'finish':
            if real_round < 1:
                raise ValueError('Game has not started')
            curs.execute('UPDATE GameConfig SET game_running=FALSE WHERE id=1')
            # Unpublished jobs cannot drain after the scheduler has stopped.
            # A late delivery is fenced by the existing finished_at check.
            # A renewed lease means a worker already started before mark_sent.
            curs.execute(
                'UPDATE CheckerJobs SET finished_at=clock_timestamp() '
                'WHERE sent_at IS NULL AND deadline_at IS NULL '
                'AND finished_at IS NULL',
            )
        set_game_paused(action != 'resume')
        conn.commit()


def finalize_finished_game() -> None:
    """Publish the last round once all already sent checks have settled."""
    if not is_game_finished():
        return
    redis = utils.RedisStorage.get()
    if redis.exists('game:final_snapshot') or has_pending_checks():
        return
    update_game_state(get_real_round_from_db())
    redis.set('game:final_snapshot', 1)


def get_runtime_status() -> dict:
    with utils.redis_pipeline(transaction=False) as pipe:
        paused_at, paused_seconds = pipe.get(PAUSED_AT).get(PAUSED_SECONDS).execute()
    real_round, running = get_lifecycle()
    finished = real_round > 0 and not running
    phase = (
        'finished' if finished else
        'paused' if paused_at else
        'running' if real_round >= 1 else 'waiting'
    )
    return {
        'phase': phase,
        'round': real_round,
        'round_start': get_round_start(real_round) if real_round else None,
        'paused_at': float(paused_at) if paused_at else None,
        'paused_seconds': float(paused_seconds or 0),
        'round_time': get_current_game_config().round_time,
        'round_waiting': bool(utils.RedisStorage.get().exists('game:round_waiting')),
        'results_pending': finished and has_pending_checks(),
    }
