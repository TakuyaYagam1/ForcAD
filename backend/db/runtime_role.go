package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const runtimeRoleMarker = "ForcAD managed runtime role v1"

// Runtime gets only DML on application tables. Migrations, ownership, schema
// changes and maintenance TRUNCATE remain exclusive to the bootstrap account.
var runtimeTables = []string{
	"gameconfig", "teams", "tasks", "flags", "stolenflags", "teamtasks",
	"teamtaskslog", "schedulehistory", "gamesession", "dispatchruns",
	"checkerjobs", "gameroundsnapshots", "checkeractionlogs", "publicannouncements",
}

// ProvisionRuntimeRole runs only on the explicit initializer/migrate connection.
// It preserves existing game data and refuses to repurpose an unrelated role.
func ProvisionRuntimeRole(ctx context.Context, databaseURL, username, password string) error {
	const location = "db.ProvisionRuntimeRole"
	if err := validateRoleCredentials(username, password); err != nil {
		return err
	}
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("%s - connect privileged initializer: %w", location, err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = conn.Close(cleanup)
	}()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%s - begin role provisioning: %w", location, err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	// Serialize repeat initializer runs without locking normal runtime traffic.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(718221947)`); err != nil {
		return fmt.Errorf("%s - lock provisioning: %w", location, err)
	}
	var admin, database, schema string
	var databaseOID, schemaOID uint32
	if err := tx.QueryRow(ctx, `SELECT current_user, current_database(), current_schema(), (SELECT oid FROM pg_database WHERE datname=current_database()), (SELECT oid FROM pg_namespace WHERE nspname=current_schema())`).Scan(&admin, &database, &schema, &databaseOID, &schemaOID); err != nil {
		return fmt.Errorf("%s - identify migration owner: %w", location, err)
	}
	if username == admin {
		return fmt.Errorf("%s - runtime user must differ from migration owner", location)
	}
	marker := fmt.Sprintf("%s:%d:%d", runtimeRoleMarker, databaseOID, schemaOID)
	if err := ensureRuntimeRole(ctx, tx, username, marker); err != nil {
		return err
	}
	if err := validateRuntimeGrants(ctx, tx, username, schema); err != nil {
		return err
	}
	if err := setRuntimePassword(ctx, tx, username, password); err != nil {
		return err
	}
	if err := grantRuntimeTables(ctx, tx, username, database, schema); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%s - commit runtime role provisioning: %w", location, err)
	}
	return nil
}

func validateRoleCredentials(username, password string) error {
	if strings.TrimSpace(username) == "" || len(username) > 63 || strings.ContainsAny(username, "\x00\r\n") || len(password) < 16 || strings.ContainsAny(password, "\x00\r\n") {
		return fmt.Errorf("db.validateRoleCredentials - runtime credentials require a username of 1-63 bytes and a password of at least 16 characters")
	}
	return nil
}

func setRuntimePassword(ctx context.Context, tx pgx.Tx, username, password string) error {
	const location = "db.setRuntimePassword"
	var passwordSQL string
	if err := tx.QueryRow(ctx, `SELECT format('ALTER ROLE %I LOGIN PASSWORD %L', $1::text, $2::text)`, username, password).Scan(&passwordSQL); err != nil {
		return fmt.Errorf("%s - prepare runtime authentication", location)
	}
	if _, err := tx.Exec(ctx, passwordSQL); err != nil {
		// Never include a credential-bearing statement in diagnostics.
		return fmt.Errorf("%s - update runtime authentication", location)
	}
	return nil
}

func grantRuntimeTables(ctx context.Context, tx pgx.Tx, username, database, schema string) error {
	const location = "db.grantRuntimeTables"
	role := pgx.Identifier{username}.Sanitize()
	for _, statement := range []string{
		"GRANT CONNECT ON DATABASE " + pgx.Identifier{database}.Sanitize() + " TO " + role,
		"GRANT USAGE ON SCHEMA " + pgx.Identifier{schema}.Sanitize() + " TO " + role,
	} {
		if _, err := tx.Exec(ctx, statement); err != nil {
			return fmt.Errorf("%s - grant runtime connection/schema access: %w", location, err)
		}
	}
	for _, table := range runtimeTables {
		if _, err := tx.Exec(ctx, "GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE "+pgx.Identifier{schema, table}.Sanitize()+" TO "+role); err != nil {
			return fmt.Errorf("%s - grant application table access: %w", location, err)
		}
	}
	return nil
}

func ensureRuntimeRole(ctx context.Context, tx pgx.Tx, username, expectedMarker string) error {
	const location = "db.ensureRuntimeRole"
	var marker string
	var privileged bool
	err := tx.QueryRow(ctx, `SELECT COALESCE(shobj_description(r.oid, 'pg_authid'), ''),
		r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls
		OR EXISTS (SELECT 1 FROM pg_auth_members WHERE member=r.oid OR roleid=r.oid)
		OR EXISTS (SELECT 1 FROM pg_shdepend WHERE refclassid='pg_authid'::regclass AND refobjid=r.oid AND deptype='o')
		FROM pg_roles r WHERE rolname=$1`, username).Scan(&marker, &privileged)
	if errors.Is(err, pgx.ErrNoRows) {
		role := pgx.Identifier{username}.Sanitize()
		if _, err := tx.Exec(ctx, "CREATE ROLE "+role+" NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS"); err != nil {
			return fmt.Errorf("%s - create runtime role: %w", location, err)
		}
		// Marker contains only a fixed prefix and numeric database/schema OIDs.
		if _, err := tx.Exec(ctx, "COMMENT ON ROLE "+role+" IS '"+expectedMarker+"'"); err != nil {
			return fmt.Errorf("%s - bind managed role to deployment: %w", location, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s - inspect runtime role: %w", location, err)
	}
	if marker != expectedMarker || privileged {
		return fmt.Errorf("%s - refusing unmanaged, other-deployment, privileged, owning or member role; choose a new runtime username", location)
	}
	return nil
}

func validateRuntimeGrants(ctx context.Context, tx pgx.Tx, username, schema string) error {
	const location = "db.validateRuntimeGrants"
	var excessive bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE has_schema_privilege($1, oid, 'CREATE'))
		OR has_database_privilege($1, current_database(), 'CREATE')
		OR has_table_privilege($1, format('%I.goose_db_version', $2::text), 'SELECT,INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER')
		OR EXISTS (
			SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace,
			LATERAL aclexplode(CASE WHEN array_ndims(c.relacl)=1 THEN c.relacl END) a
			WHERE a.grantee=(SELECT oid FROM pg_roles WHERE rolname=$1)
			AND (n.nspname<>$2 OR c.relname<>ALL($3::text[]) OR a.is_grantable
			OR a.privilege_type NOT IN ('SELECT','INSERT','UPDATE','DELETE'))
		)
		OR EXISTS (
			SELECT 1 FROM pg_shdepend d
			WHERE d.refclassid='pg_authid'::regclass AND d.refobjid=(SELECT oid FROM pg_roles WHERE rolname=$1)
			AND d.deptype IN ('a','r') AND NOT (
				(d.dbid=(SELECT oid FROM pg_database WHERE datname=current_database()) AND d.objsubid=0 AND (
					(d.classid='pg_class'::regclass AND d.objid IN (SELECT c.oid FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=$2 AND c.relname=ANY($3::text[])))
					OR (d.classid='pg_namespace'::regclass AND d.objid=(SELECT oid FROM pg_namespace WHERE nspname=$2))
				))
				OR (d.dbid=0 AND d.classid='pg_database'::regclass AND d.objsubid=0 AND d.objid=(SELECT oid FROM pg_database WHERE datname=current_database()))
			)
		)
		OR EXISTS (
			SELECT 1 FROM pg_namespace n, LATERAL aclexplode(CASE WHEN array_ndims(n.nspacl)=1 THEN n.nspacl END) a
			WHERE a.grantee=(SELECT oid FROM pg_roles WHERE rolname=$1)
			AND (a.is_grantable OR a.privilege_type<>'USAGE' OR n.nspname<>$2)
		)
		OR EXISTS (
			SELECT 1 FROM pg_database d, LATERAL aclexplode(CASE WHEN array_ndims(d.datacl)=1 THEN d.datacl END) a
			WHERE a.grantee=(SELECT oid FROM pg_roles WHERE rolname=$1)
			AND (a.is_grantable OR a.privilege_type<>'CONNECT' OR d.datname<>current_database())
		)
		OR EXISTS (
			SELECT 1 FROM unnest($3::text[]) t(name),
			(VALUES ('TRUNCATE'),('REFERENCES'),('TRIGGER'),('MAINTAIN')) p(privilege)
			WHERE has_table_privilege($1, format('%I.%I', $2::text, t.name), p.privilege)
		)`, username, schema, runtimeTables).Scan(&excessive)
	if err != nil {
		return fmt.Errorf("%s - inspect effective runtime privileges: %w", location, err)
	}
	if excessive {
		return fmt.Errorf("%s - runtime role has excess object privileges or PUBLIC CREATE; correct grants before provisioning", location)
	}
	return nil
}
