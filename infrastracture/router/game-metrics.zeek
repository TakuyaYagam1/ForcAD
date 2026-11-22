# Run with zeek -b: no default protocol logs or file extraction.
@load base/frameworks/telemetry

module Forcad;

export {
    const peers: table[addr] of addr = {} &redef;
    const vulnboxes: set[addr] = {} &redef;
    const game_ports: set[port] = {} &redef;
    const profile_members: table[addr] of string = {} &redef;
    const profile_teams: table[addr] of string = {} &redef;
    const large_body_threshold: count = 1048576 &redef;
    const large_body_log_window: interval = 1min &redef;
    const large_body_log_limit: count = 100 &redef;
}

redef Log::enable_local_logging = F;
redef Log::enable_remote_logging = F;
redef truncate_http_URI = 256;

# Inactivity expiry is an estimate of liveness, not a socket count on the server.
redef tcp_inactivity_timeout = 5min;
redef udp_inactivity_timeout = 1min;

function counter(name: string, help: string): Telemetry::CounterFamily
    {
    return Telemetry::register_counter_family([
        $prefix="forcad_game", $unit="", $name=name, $help_text=help,
        $label_names=vector("source_team_ip", "team_ip")]);
    }

function gauge(name: string, help: string): Telemetry::GaugeFamily
    {
    return Telemetry::register_gauge_family([
        $prefix="forcad_game", $unit="", $name=name, $help_text=help,
        $label_names=vector("source_team_ip", "team_ip")]);
    }

function service_counter(name: string, help: string): Telemetry::CounterFamily
    {
    return Telemetry::register_counter_family([
        $prefix="forcad_game", $unit="", $name=name, $help_text=help,
        $label_names=vector("source_ip", "team_ip", "service_port")]);
    }

function service_gauge(name: string, help: string): Telemetry::GaugeFamily
    {
    return Telemetry::register_gauge_family([
        $prefix="forcad_game", $unit="", $name=name, $help_text=help,
        $label_names=vector("source_ip", "team_ip", "service_port")]);
    }

function observation_counter(): Telemetry::CounterFamily
    {
    return Telemetry::register_counter_family([
        $prefix="forcad_game", $unit="", $name="observations",
        $help_text="Observed TCP initial outcomes, established resets, and final plaintext HTTP responses.",
        $label_names=vector("source_ip", "team_ip", "service_port", "outcome")]);
    }

global http_requests = counter("http_requests", "Observed HTTP/1 request lines, including upgrade requests.");
global ws_upgrades = counter("websocket_upgrades", "Observed HTTP 101 upgrades to WebSocket.");
global tcp_flows = counter("tcp_flows", "New observed TCP flows, including incomplete handshakes.");
global tcp_established = counter("tcp_established", "Observed established TCP connections.");
global udp_flows = counter("udp_flows", "New observed UDP flows; inactivity timeout is 60 seconds.");
global gaps = counter("stream_gaps", "Observed TCP stream gaps; HTTP/WS counts may be incomplete.");
global tcp_active = gauge("tcp_connections", "Observed established TCP connections until close or 5 minute inactivity.");
global ws_active = gauge("websocket_connections", "Observed upgraded TCP connections until close or 5 minute inactivity.");
global udp_active = gauge("udp_flows_active", "Observed UDP flows until 60 second inactivity.");
global peer_http_requests = service_counter("peer_http_requests", "Observed HTTP/1 request lines from commissioned peers.");
global http_large_bodies = service_counter("http_large_bodies", "Completed originator HTTP/1 request bodies at or above the configured byte threshold.");
global http_last_large_body_bytes = service_gauge("http_last_large_body_bytes", "Decoded request body size in bytes for the most recently observed large body.");
global http_last_large_body_timestamp = service_gauge("http_last_large_body_timestamp_seconds", "Unix timestamp of the most recently observed large request body.");
global observations = observation_counter();

global large_body_log_window_started: time = double_to_time(0.0);
global large_body_log_count: count = 0;

global captured = Telemetry::counter_with(Telemetry::register_counter_family([
    $prefix="forcad_game", $unit="", $name="capture_packets", $help_text="Packets received by the sensor."]));
global dropped = Telemetry::counter_with(Telemetry::register_counter_family([
    $prefix="forcad_game", $unit="", $name="capture_dropped_packets", $help_text="Capture drops reported by the system."]));
global heartbeat = Telemetry::gauge_with(Telemetry::register_gauge_family([
    $prefix="forcad_game", $unit="", $name="sync_timestamp_seconds", $help_text="Last main-loop metric sync, Unix time."]));
global peer_info = Telemetry::register_gauge_family([
    $prefix="forcad_game", $unit="", $name="peer_info", $help_text="Configured VPN peer to source team mapping.",
    $label_names=vector("source_ip", "source_team_ip")]);
global profile_info = Telemetry::register_gauge_family([
    $prefix="forcad_game", $unit="", $name="profile_info",
    $help_text="Configured VPN peer to team and participant profile mapping.",
    $label_names=vector("source_ip", "source_team_ip", "source_team_name", "member")]);

type Flow: record {
    labels: vector of string;
    service_labels: vector of string;
    tcp: bool &default=F;
    ws: bool &default=F;
    udp: bool &default=F;
    initial_outcome: bool &default=F;
    reset_recorded: bool &default=F;
    origin_gap: bool &default=F;
};
redef record connection += { forcad: Flow &optional; };

event zeek_init()
    {
    # An empty handle tells the file manager not to create a file for the body.
    Files::register_protocol(Analyzer::ANALYZER_HTTP,
        [$get_file_handle=function(c: connection, is_orig: bool): string { return ""; }]);
    local tcp_ports: set[port];
    for ( p in game_ports )
        if ( get_port_transport_proto(p) == tcp )
            add tcp_ports[p];
    Analyzer::register_for_ports(Analyzer::ANALYZER_HTTP, tcp_ports);

    # Only inventory pairs become metric labels. Never label URLs or payloads.
    local sources: set[addr];
    for ( peer in peers )
        {
        add sources[peers[peer]];
        Telemetry::gauge_family_set(peer_info, vector(cat(peer), cat(peers[peer])), 1.0);
        if ( peer in profile_members && peer in profile_teams )
            Telemetry::gauge_family_set(profile_info,
                vector(cat(peer), cat(peers[peer]), profile_teams[peer], profile_members[peer]), 1.0);
        }
    for ( source in sources )
        for ( target in vulnboxes )
            {
            local base_labels = vector(cat(source), cat(target));
            local counters = vector(http_requests, ws_upgrades, tcp_flows, tcp_established, udp_flows, gaps);
            for ( i in counters )
                Telemetry::counter_family_inc(counters[i], base_labels, 0.0);
            local gauges = vector(tcp_active, ws_active, udp_active);
            for ( j in gauges )
                Telemetry::gauge_family_set(gauges[j], base_labels, 0.0);
            }
    }

event new_connection(c: connection)
    {
    if ( c$id$orig_h !in peers || c$id$resp_h !in vulnboxes || c$id$resp_p !in game_ports )
        return;
    c$forcad = [
        $labels=vector(cat(peers[c$id$orig_h]), cat(c$id$resp_h)),
        $service_labels=vector(cat(c$id$orig_h), cat(c$id$resp_h), cat(c$id$resp_p))];
    if ( get_port_transport_proto(c$id$resp_p) == tcp )
        Telemetry::counter_family_inc(tcp_flows, c$forcad$labels);
    else if ( get_port_transport_proto(c$id$resp_p) == udp )
        {
        c$forcad$udp = T;
        Telemetry::counter_family_inc(udp_flows, c$forcad$labels);
        Telemetry::gauge_family_inc(udp_active, c$forcad$labels);
        }
    }

function record_initial_outcome(c: connection, outcome: string)
    {
    if ( ! c?$forcad || c$forcad$initial_outcome ||
         get_port_transport_proto(c$id$resp_p) != tcp )
        return;
    Telemetry::counter_family_inc(observations,
        vector(cat(c$id$orig_h), cat(c$id$resp_h), cat(c$id$resp_p), outcome));
    c$forcad$initial_outcome = T;
    }

event connection_established(c: connection)
    {
    if ( ! c?$forcad || c$forcad$tcp )
        return;
    c$forcad$tcp = T;
    Telemetry::counter_family_inc(tcp_established, c$forcad$labels);
    Telemetry::gauge_family_inc(tcp_active, c$forcad$labels);
    record_initial_outcome(c, "tcp_established");
    }

event connection_attempt(c: connection)
    {
    record_initial_outcome(c, "tcp_no_reply");
    }

event connection_rejected(c: connection)
    {
    record_initial_outcome(c, "tcp_refused");
    }

function close_flow(c: connection)
    {
    if ( ! c?$forcad )
        return;
    if ( c$forcad$tcp )
        Telemetry::gauge_family_dec(tcp_active, c$forcad$labels);
    if ( c$forcad$ws )
        Telemetry::gauge_family_dec(ws_active, c$forcad$labels);
    if ( c$forcad$udp )
        Telemetry::gauge_family_dec(udp_active, c$forcad$labels);
    c$forcad$tcp = F;
    c$forcad$ws = F;
    c$forcad$udp = F;
    }
event connection_reset(c: connection)
    {
    if ( c?$forcad && c$forcad$tcp && ! c$forcad$reset_recorded )
        {
        Telemetry::counter_family_inc(observations,
            vector(cat(c$id$orig_h), cat(c$id$resp_h), cat(c$id$resp_p), "tcp_reset"));
        c$forcad$reset_recorded = T;
        }
    close_flow(c);
    }

event http_request(c: connection, method: string, original_URI: string, unescaped_URI: string, version: string)
    {
    if ( c?$forcad )
        {
        Telemetry::counter_family_inc(http_requests, c$forcad$labels);
        Telemetry::counter_family_inc(peer_http_requests, c$forcad$service_labels);
        }
    }

event http_reply(c: connection, version: string, code: count, reason: string)
    {
    if ( ! c?$forcad || (code >= 100 && code < 200 && code != 101) )
        return;
    local outcome = "http_other";
    if ( code == 403 )
        outcome = "http_403";
    else if ( code == 429 )
        outcome = "http_429";
    Telemetry::counter_family_inc(observations,
        vector(cat(c$id$orig_h), cat(c$id$resp_h), cat(c$id$resp_p), outcome));
    }

function log_large_body(c: connection, size: count, observed_at: time)
    {
    if ( large_body_log_limit == 0 || large_body_log_window <= 0sec )
        return;
    if ( large_body_log_window_started == double_to_time(0.0) ||
         observed_at >= large_body_log_window_started + large_body_log_window )
        {
        large_body_log_window_started = observed_at;
        large_body_log_count = 0;
        }
    if ( large_body_log_count >= large_body_log_limit )
        return;
    ++large_body_log_count;
    print fmt(
        "FORCAD_LARGE_HTTP_BODY ts=%s source=%s:%s target=%s:%s protocol=tcp bytes=%s",
        strftime("%Y-%m-%dT%H:%M:%SZ", observed_at), cat(c$id$orig_h),
        cat(c$id$orig_p), cat(c$id$resp_h), cat(c$id$resp_p), cat(size));
    }

event http_message_done(c: connection, is_orig: bool, stat: http_message_stat)
    {
    if ( ! is_orig || ! c?$forcad || c$forcad$origin_gap ||
         stat$interrupted || stat$content_gap_length > 0 ||
         stat$body_length < large_body_threshold )
        return;
    local observed_at = network_time();
    Telemetry::counter_family_inc(http_large_bodies, c$forcad$service_labels);
    Telemetry::gauge_family_set(http_last_large_body_bytes,
        c$forcad$service_labels, count_to_double(stat$body_length));
    Telemetry::gauge_family_set(http_last_large_body_timestamp,
        c$forcad$service_labels, time_to_double(observed_at));
    log_large_body(c, stat$body_length, observed_at);
    }

event http_connection_upgrade(c: connection, protocol: string)
    {
    if ( ! c?$forcad || c$forcad$ws || to_lower(protocol) != "websocket" )
        return;
    c$forcad$ws = T;
    Telemetry::counter_family_inc(ws_upgrades, c$forcad$labels);
    Telemetry::gauge_family_inc(ws_active, c$forcad$labels);
    }

event content_gap(c: connection, is_orig: bool, seq: count, length: count)
    {
    if ( c?$forcad )
        {
        Telemetry::counter_family_inc(gaps, c$forcad$labels);
        if ( is_orig )
            c$forcad$origin_gap = T;
        }
    }

event connection_finished(c: connection) { close_flow(c); }
event connection_state_remove(c: connection) { close_flow(c); }

hook Telemetry::sync()
    {
    local stats = get_net_stats();
    Telemetry::counter_set(captured, count_to_double(stats$pkts_recvd));
    Telemetry::counter_set(dropped, count_to_double(stats$pkts_dropped));
    Telemetry::gauge_set(heartbeat, time_to_double(current_time()));
    }
