from lib import models, storage
from lib.helpers.cache import cache_helper
from lib.storage.keys import CacheKeys


def get_teams() -> list[models.Team]:
    """Get list of active teams."""
    key = CacheKeys.teams()
    with storage.utils.redis_pipeline(transaction=True) as pipe:
        cache_helper(
            pipeline=pipe,
            cache_key=key,
            cache_func=storage.caching.cache_teams,
            cache_args=(pipe,),
        )

        (teams,) = pipe.smembers(key).execute()
        teams = list(models.Team.from_json(team) for team in teams)

    return teams


def get_all_teams() -> list[models.Team]:
    """Get list of all teams, including inactive."""
    with storage.utils.db_cursor(dict_cursor=True) as (_, curs):
        curs.execute(models.Team.get_select_all_query())
        teams = curs.fetchall()

    teams = list(models.Team.from_dict(team) for team in teams)
    return teams


def get_team_id_by_token(token: str) -> int | None:
    """
    Get team by token.

    :param token: token string
    :return: team id
    """
    # The DB is authoritative: changing a token or disabling a team takes effect
    # immediately, even if Redis contains old credential mappings.
    with storage.utils.db_cursor() as (_, curs):
        curs.execute(
            'SELECT id FROM Teams WHERE token=%(token)s AND active=TRUE',
            {'token': token},
        )
        row = curs.fetchone()
    return int(row[0]) if row else None


def create_team(team: models.Team) -> models.Team:
    """Add new team to DB, reset cache & return created instance."""
    with storage.utils.db_cursor() as (conn, curs):
        team.insert(curs)

        insert_data = (
            {
                'task_id': task.id,
                'team_id': team.id,
                'score': task.default_score,
                'status': -1,
            }
            for task in storage.tasks.get_all_tasks()
        )
        curs.executemany(storage.tasks.TEAMTASK_INSERT_QUERY, insert_data)

        conn.commit()

    storage.caching.flush_teams_cache()
    return team


def update_team(team: models.Team) -> models.Team:
    """Update team, reset cache & return updated instance."""
    with storage.utils.db_cursor() as (conn, curs):
        curs.execute(team.get_update_query(), team.to_dict())
        conn.commit()

    storage.caching.flush_teams_cache()
    return team


def delete_team(team_id: int) -> None:
    """Set active = False on a team."""
    with storage.utils.db_cursor() as (conn, curs):
        curs.execute(models.Team.get_delete_query(), {'id': team_id})
        conn.commit()

    storage.caching.flush_teams_cache()
