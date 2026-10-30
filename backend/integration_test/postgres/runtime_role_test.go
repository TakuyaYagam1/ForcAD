package postgres_test

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/TakuyaYagam1/ForcAD/db"
	"github.com/TakuyaYagam1/ForcAD/integration_test/testutil"
	"github.com/jackc/pgx/v5"
)

func TestRuntimeRolePreservesDataAndRestrictsPrivileges(t *testing.T) {
	pool, raw := testutil.Database(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	name := "forcad_runtime_" + strings.ReplaceAll(uuid.NewV7().String(), "-", "")
	const password = "isolated-test-runtime-password"
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		// The random test-only role owns no objects. Remove its grants before it.
		var exists bool
		if err := pool.QueryRow(cleanup, `SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)`, name).Scan(&exists); err != nil {
			t.Error(err)
			return
		}
		if !exists {
			return
		}
		if _, err := pool.Exec(cleanup, "DROP OWNED BY "+pgx.Identifier{name}.Sanitize()); err != nil {
			t.Error(err)
		}
		if _, err := pool.Exec(cleanup, "DROP ROLE IF EXISTS "+pgx.Identifier{name}.Sanitize()); err != nil {
			t.Error(err)
		}
	})
	for range 2 {
		if err := db.ProvisionRuntimeRole(ctx, raw, name, password); err != nil {
			t.Fatal(err)
		}
	}
	address, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	address.User = url.UserPassword(name, password)
	runtime, err := pgx.Connect(ctx, address.String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = runtime.Close(context.Background()) }()
	var allowed, forbidden bool
	if err := runtime.QueryRow(ctx, `SELECT
		(SELECT bool_and(has_table_privilege(current_user, t.name, p.privilege))
		 FROM (VALUES ('teams'),('checkerjobs')) t(name),
		 (VALUES ('SELECT'),('INSERT'),('UPDATE'),('DELETE')) p(privilege)),
		has_table_privilege(current_user, 'goose_db_version', 'UPDATE')
		OR has_table_privilege(current_user, 'teams', 'TRUNCATE')
		OR has_schema_privilege(current_user, current_schema(), 'CREATE')
		OR (SELECT rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls FROM pg_roles WHERE rolname=current_user)`).Scan(&allowed, &forbidden); err != nil {
		t.Fatal(err)
	}
	if !allowed || forbidden {
		t.Fatalf("runtime DML=%v excessive privileges=%v", allowed, forbidden)
	}
	if _, err := runtime.Exec(ctx, `INSERT INTO Teams(id,name,ip,token) VALUES($1,'Existing team','192.0.2.8','0123456789abcdef')`, uuid.NewV7()); err != nil {
		t.Fatal(err)
	}
	if err := db.ProvisionRuntimeRole(ctx, raw, name, password); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := runtime.QueryRow(ctx, `SELECT count(*) FROM Teams`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("preserved rows=%d: %v", count, err)
	}
	// A marked role must not be adopted by a second deployment in this cluster.
	_, other := testutil.Database(t)
	if err := db.ProvisionRuntimeRole(ctx, other, name, "different-deployment-password"); err == nil {
		t.Fatal("accepted another deployment's managed role")
	}
	// Configuration drift must fail closed, rather than preserve excess grants.
	if _, err := pool.Exec(ctx, "GRANT TRUNCATE ON Teams TO "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if err := db.ProvisionRuntimeRole(ctx, raw, name, password); err == nil {
		t.Fatal("accepted excess runtime TRUNCATE grant")
	}
}

func TestRuntimeRoleRefusesBootstrapIdentity(t *testing.T) {
	_, raw := testutil.Database(t)
	address, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ProvisionRuntimeRole(t.Context(), raw, address.User.Username(), "isolated-test-password"); err == nil {
		t.Fatal("accepted migration owner as runtime role")
	}
}
