"""Non-destructive schema update for an already initialized ForcAD database."""

from pathlib import Path

from lib.storage import utils


def main():
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
        cursor.execute(Path(__file__).with_name('create_functions.sql').read_text())
        conn.commit()
    print(
        'Schema updated. Existing teams, scores and history retained. '
        'Team tokens were validated without modification.'
    )


if __name__ == '__main__':
    main()
