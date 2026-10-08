"""Non-destructive schema update for an already initialized ForcAD database."""

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
            ALTER TABLE GameConfig
                DROP CONSTRAINT IF EXISTS gameconfig_game_hardness_check;
            ALTER TABLE GameConfig
                ADD CONSTRAINT gameconfig_game_hardness_check
                CHECK (game_hardness > 1 AND game_hardness < 'Infinity'::float8);
            ALTER TABLE Teams ALTER COLUMN ip TYPE VARCHAR(45);
        """
        )
        conn.commit()
    print('Schema updated. Existing teams, scores and history retained.')


if __name__ == '__main__':
    main()
