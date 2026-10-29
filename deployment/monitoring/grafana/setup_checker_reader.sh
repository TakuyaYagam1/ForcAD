#!/bin/sh
set -eu

case "${GRAFANA_CHECKER_DB_PASSWORD:-}" in
    ''|*[!0-9a-f]*)
        echo 'GRAFANA_CHECKER_DB_PASSWORD must be lowercase hexadecimal' >&2
        exit 1
        ;;
esac
if [ "${#GRAFANA_CHECKER_DB_PASSWORD}" -lt 32 ]; then
    echo 'GRAFANA_CHECKER_DB_PASSWORD must be at least 32 characters' >&2
    exit 1
fi

export PGHOST=postgres PGPORT=5432 PGUSER="$POSTGRES_USER" \
    PGDATABASE="$POSTGRES_DB" PGPASSWORD="$POSTGRES_PASSWORD"

existing=$(psql -X -v ON_ERROR_STOP=1 -Atqc \
    "SELECT count(*) FROM pg_class WHERE relname IN ('checker_action_events', 'checker_services', 'checker_teams') AND relkind = 'v'")
views=$(awk '
    /^CREATE VIEW checker_(action_events|services|teams) AS$/ { selected = 1; count++ }
    selected { print; if ($0 ~ /;$/) selected = 0 }
    END { if (count != 3) exit 1 }
' /initial-schema.sql)
case "$existing" in
    0)
        printf '%s\n' "$views" | psql -X -v ON_ERROR_STOP=1 -q
        ;;
    3)
        printf '%s\n' "$views" | sed 's/^CREATE VIEW /CREATE OR REPLACE VIEW /' | \
            psql -X -v ON_ERROR_STOP=1 -q
        ;;
    *) echo 'Checker views are partially installed; inspect database ownership' >&2; exit 1 ;;
esac

{
    printf "\\set checker_password '%s'\n" "$GRAFANA_CHECKER_DB_PASSWORD"
    cat <<'SQL'
DO $$
DECLARE
    reader_oid OID;
BEGIN
    SELECT oid INTO reader_oid FROM pg_roles WHERE rolname = 'forcad_checker_reader';
    IF reader_oid IS NULL THEN
        CREATE ROLE forcad_checker_reader LOGIN;
        COMMENT ON ROLE forcad_checker_reader IS 'ForcAD Checker dashboard reader';
    ELSIF shobj_description(reader_oid, 'pg_authid') IS DISTINCT FROM 'ForcAD Checker dashboard reader' THEN
        RAISE EXCEPTION 'role forcad_checker_reader is owned by another configuration';
    END IF;
END;
$$;

SELECT format('ALTER ROLE forcad_checker_reader PASSWORD %L', :'checker_password') \gexec
GRANT USAGE ON SCHEMA public TO forcad_checker_reader;
GRANT SELECT ON public.checker_action_events, public.checker_services, public.checker_teams
    TO forcad_checker_reader;
SQL
} | psql -X -v ON_ERROR_STOP=1 -q
