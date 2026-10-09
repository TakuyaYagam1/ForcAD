"""Non-destructive schema update for an already initialized ForcAD database."""

import argparse
from pathlib import Path

import yaml

from lib.models.game_config import validate_rounds
from lib.storage import utils
from lib.storage.keys import CacheKeys


def main(*, config_path: Path | None = None):
    # Opt in to applying only the round limit from an existing configuration.
    # Never reload start_time, credentials, teams or scores during a migration.
    if config_path is not None:
        with config_path.open() as stream:
            rounds = validate_rounds(yaml.safe_load(stream)['game'].get('rounds'))
    with utils.db_cursor() as (conn, cursor):
        cursor.execute('SELECT game_hardness FROM GameConfig WHERE id=1')
        row = cursor.fetchone()
        if row and (row[0] is None or not 1 < row[0] < float('inf')):
            raise ValueError(
                'Set game_hardness > 1 in the DB and configuration before '
                'applying this update. No changes made.'
            )

        cursor.execute(
            """
            SELECT column_name
            FROM information_schema.columns
            WHERE table_schema = current_schema()
              AND table_name = 'teams'
            """
        )
        team_columns = {result[0] for result in cursor.fetchall()}
        if 'token' not in team_columns:
            raise ValueError(
                'Teams.token is missing. This database is too old for the '
                'token migration; no changes made.'
            )

        cursor.execute('LOCK TABLE Teams IN ACCESS EXCLUSIVE MODE')
        cursor.execute(
            """
            SELECT id
            FROM Teams
            WHERE token IS NULL OR token !~ '^[0-9a-f]{16}$'
            ORDER BY id
            LIMIT 20
            """
        )
        invalid_token_ids = [result[0] for result in cursor.fetchall()]
        cursor.execute(
            """
            SELECT array_agg(id ORDER BY id)
            FROM Teams
            GROUP BY token
            HAVING COUNT(*) > 1
            ORDER BY MIN(id)
            LIMIT 20
            """
        )
        duplicate_team_ids = [result[0] for result in cursor.fetchall()]
        if invalid_token_ids or duplicate_team_ids:
            details = []
            if invalid_token_ids:
                details.append(f'invalid token rows (IDs): {invalid_token_ids}')
            if duplicate_team_ids:
                details.append(f'duplicate token rows (IDs): {duplicate_team_ids}')
            message = (
                'Cannot enforce team token format and uniqueness: '
                f'{"; ".join(details)}'
                '. Resolve these rows manually; no changes made.'
            )
            raise ValueError(message)

        cursor.execute(
            """
            SELECT 1 FROM pg_constraint
            WHERE conrelid = 'teams'::regclass
              AND conname = 'teams_token_format_check'
            """
        )
        has_token_format_check = cursor.fetchone() is not None

        cursor.execute(
            """
            ALTER TABLE GameConfig
                DROP CONSTRAINT IF EXISTS gameconfig_game_hardness_check;
            ALTER TABLE GameConfig
                ADD CONSTRAINT gameconfig_game_hardness_check
                CHECK (game_hardness > 1 AND game_hardness < 'Infinity'::float8);
            ALTER TABLE GameConfig
                ADD COLUMN IF NOT EXISTS rounds INTEGER CHECK (rounds > 0);
            ALTER TABLE Teams ALTER COLUMN ip TYPE VARCHAR(45);
            ALTER TABLE Teams
                ADD COLUMN IF NOT EXISTS logo_path VARCHAR(255) DEFAULT '';
            ALTER TABLE Teams ALTER COLUMN token TYPE VARCHAR(16);
            ALTER TABLE Teams ALTER COLUMN token SET NOT NULL;
            ALTER TABLE Teams ALTER COLUMN token DROP DEFAULT;
        """
        )

        if not has_token_format_check:
            cursor.execute(
                """
                ALTER TABLE Teams
                    ADD CONSTRAINT teams_token_format_check
                    CHECK (token ~ '^[0-9a-f]{16}$')
                """
            )

        cursor.execute(
            """
            SELECT 1
            FROM information_schema.table_constraints AS tc
            JOIN information_schema.key_column_usage AS kcu
              USING (constraint_catalog, constraint_schema, constraint_name,
                     table_catalog, table_schema, table_name)
            WHERE tc.table_schema = current_schema()
              AND tc.table_name = 'teams'
              AND tc.constraint_type = 'UNIQUE'
            GROUP BY tc.constraint_name
            HAVING COUNT(*) = 1 AND BOOL_AND(kcu.column_name = 'token')
            LIMIT 1
            """
        )
        if cursor.fetchone() is None:
            cursor.execute(
                'ALTER TABLE Teams ADD CONSTRAINT teams_token_unique UNIQUE (token)'
            )
        cursor.execute(Path(__file__).with_name('create_dispatch.sql').read_text())
        cursor.execute(Path(__file__).with_name('create_sessions.sql').read_text())
        cursor.execute(Path(__file__).with_name('create_functions.sql').read_text())
        if config_path is not None:
            cursor.execute('UPDATE GameConfig SET rounds=%s WHERE id=1', (rounds,))
        conn.commit()
    if config_path is not None:
        utils.RedisStorage.get().delete(CacheKeys.game_config())
        print(f'Round limit: {rounds if rounds is not None else "unlimited"}.')
    print(
        'Schema updated. Existing teams, scores and history retained. '
        'Team tokens were validated without modification.'
    )


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        '--config', type=Path,
        help='Apply game.rounds from this YAML file; missing/null means unlimited',
    )
    args = parser.parse_args()
    main(config_path=args.config)
