function fail(message) {
    print "PgBouncer configuration: " message
    exit 1
}
function value(key, fallback, result) {
    result = ENVIRON[key]
    if (result == "") result = fallback
    if (result ~ /[\r\n]/) fail(key " must be a single line")
    return result
}
function number(key, fallback, result) {
    result = value(key, fallback)
    if (result !~ /^[0-9]+$/ || result+0 < 1 || result+0 > 65535) fail(key " must be between 1 and 65535")
    return result+0
}
function quoted(text) {
    gsub(/'/, "''", text)
    return "'" text "'"
}
function authQuoted(text) {
    gsub(/"/, "\"\"", text)
    return "\"" text "\""
}
function maximum(a, b) { return a > b ? a : b }
function replicaNumber(key, fallback, result) {
    result = value(key, fallback)
    if (result !~ /^[0-9]+$/ || result+0 > 1024) fail(key " must be between 0 and 1024")
    return result+0
}
function tlsPath(key, path, line, status) {
    path = value(key, "")
    if (path !~ /^\/[A-Za-z0-9_.\/-]+$/) fail(key " must name an absolute certificate/key file")
    status = (getline line < path)
    close(path)
    if (status < 0) fail(key " must be readable")
    return path
}
function configureTLS(client, server) {
    client = value("PGBOUNCER_CLIENT_TLS_SSLMODE", "disable")
    server = value("PGBOUNCER_SERVER_TLS_SSLMODE", "prefer")
    if (client != "disable" && client != "require") fail("PGBOUNCER_CLIENT_TLS_SSLMODE must be disable or require")
    if (server != "disable" && server != "prefer" && server != "verify-full") fail("PGBOUNCER_SERVER_TLS_SSLMODE must be disable, prefer or verify-full")
    print "client_tls_sslmode = " client "\nserver_tls_sslmode = " server > config
    if (client == "require") {
        print "client_tls_cert_file = " tlsPath("PGBOUNCER_CLIENT_TLS_CERT_FILE") > config
        print "client_tls_key_file = " tlsPath("PGBOUNCER_CLIENT_TLS_KEY_FILE") > config
    }
    if (server == "verify-full") print "server_tls_ca_file = " tlsPath("PGBOUNCER_SERVER_TLS_CA_FILE") > config
}
BEGIN {
    config = "/run/pgbouncer/pgbouncer.ini"
    auth = "/run/pgbouncer/users.txt"
    user = value("POSTGRES_USER", "forcad")
    password = value("POSTGRES_PASSWORD", "")
    if (password == "") fail("POSTGRES_PASSWORD is required")
    # Keep credentials out of arguments, logs and the image. The entrypoint
    # writes both files under umask 077 in a container-private tmpfs.
    print authQuoted(user) " " authQuoted(password) > auth
    close(auth)
    host = quoted(value("POSTGRES_HOST", "postgres"))
    database = quoted(value("POSTGRES_DB", "forcad"))
    port = number("POSTGRES_PORT", 5432)
    sizes["api"] = number("PGBOUNCER_API_CONNECTIONS", 32)
    api_min = value("PGBOUNCER_API_MIN_CONNECTIONS", 0)
    if (api_min !~ /^[0-9]+$/ || api_min+0 > sizes["api"]) fail("PGBOUNCER_API_MIN_CONNECTIONS must be between 0 and PGBOUNCER_API_CONNECTIONS")
    sizes["receiver"] = number("PGBOUNCER_RECEIVER_CONNECTIONS", 64)
    sizes["worker"] = number("PGBOUNCER_WORKER_CONNECTIONS", 32)
    sizes["ticker"] = number("PGBOUNCER_TICKER_CONNECTIONS", 8)
    replicas["api"] = replicaNumber("FORCAD_API_REPLICAS", 4)
    replicas["receiver"] = replicaNumber("FORCAD_RECEIVER_REPLICAS", 2)
    replicas["worker"] = replicaNumber("FORCAD_WORKER_REPLICAS", 2)
    replicas["ticker"] = replicaNumber("FORCAD_TICKER_REPLICAS", 1)
    app_sizes["api"] = number("FORCAD_API_DATABASE_MAX_CONNS", 16)
    app_sizes["receiver"] = number("FORCAD_RECEIVER_DATABASE_MAX_CONNS", 32)
    app_sizes["worker"] = number("FORCAD_WORKER_DATABASE_MAX_CONNS", 16)
    app_sizes["ticker"] = number("FORCAD_TICKER_DATABASE_MAX_CONNS", 8)
    max_clients = number("PGBOUNCER_MAX_CLIENTS", 1024)
    required_total = 0
    for (role in sizes) {
        required = replicas[role] * app_sizes[role] + 8
        key = "PGBOUNCER_" toupper(role) "_CLIENTS"
        clients[role] = number(key, maximum(sizes[role], maximum(256, required)))
        if (clients[role] < required || clients[role] < sizes[role]) fail(key " is smaller than the declared replica/client or server budget")
        required_total += required
    }
    if (max_clients < required_total) fail("PGBOUNCER_MAX_CLIENTS is smaller than aggregate declared application demand")
    print "[databases]" > config
    for (role in sizes) {
        minimum = role == "api" ? sprintf(" min_pool_size=%d", api_min) : ""
        printf "forcad_%s = host=%s port=%s dbname=%s pool_size=%d max_db_connections=%d%s max_db_client_connections=%d connect_query='SET statement_timeout=5000; SET lock_timeout=2000; SET idle_in_transaction_session_timeout=10000'\n", role, host, port, database, sizes[role], sizes[role], minimum, clients[role] > config
    }
    print "\n[pgbouncer]" > config
    print "listen_addr = 0.0.0.0\nlisten_port = 6432\nunix_socket_dir = /run/pgbouncer" > config
    print "auth_type = scram-sha-256\nauth_file = " auth > config
    print "stats_users = " user > config
    print "pool_mode = transaction\nmax_client_conn = " max_clients "\nmax_prepared_statements = 512" > config
    print "query_wait_timeout = 2\nquery_timeout = 6\ntransaction_timeout = 10\nidle_transaction_timeout = 5" > config
    print "server_connect_timeout = 3\nserver_login_retry = 1\nserver_idle_timeout = 60" > config
    print "server_lifetime = 1800" > config
    configureTLS()
    print "log_connections = 0\nlog_disconnections = 0\nlog_pooler_errors = 1\nstats_period = 30" > config
    close(config)
}
