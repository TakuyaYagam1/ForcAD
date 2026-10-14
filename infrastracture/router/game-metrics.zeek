# Run with zeek -b: no default protocol logs or file extraction.
@load base/frameworks/telemetry

module Forcad;

export {
    const peers: table[addr] of addr = {} &redef;
    const vulnboxes: set[addr] = {} &redef;
    const game_ports: set[port] = {} &redef;
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

global http_requests = counter("http_requests", "Observed HTTP/1 request lines, including upgrade requests.");
global ws_upgrades = counter("websocket_upgrades", "Observed HTTP 101 upgrades to WebSocket.");
global tcp_flows = counter("tcp_flows", "New observed TCP flows, including incomplete handshakes.");
global tcp_established = counter("tcp_established", "Observed established TCP connections.");
global udp_flows = counter("udp_flows", "New observed UDP flows; inactivity timeout is 60 seconds.");
global gaps = counter("stream_gaps", "Observed TCP stream gaps; HTTP/WS counts may be incomplete.");
global tcp_active = gauge("tcp_connections", "Observed established TCP connections until close or 5 minute inactivity.");
global ws_active = gauge("websocket_connections", "Observed upgraded TCP connections until close or 5 minute inactivity.");
global udp_active = gauge("udp_flows_active", "Observed UDP flows until 60 second inactivity.");

global captured = Telemetry::counter_with(Telemetry::register_counter_family([
    $prefix="forcad_game", $unit="", $name="capture_packets", $help_text="Packets received by the sensor."]));
global dropped = Telemetry::counter_with(Telemetry::register_counter_family([
    $prefix="forcad_game", $unit="", $name="capture_dropped_packets", $help_text="Capture drops reported by the system."]));
global heartbeat = Telemetry::gauge_with(Telemetry::register_gauge_family([
    $prefix="forcad_game", $unit="", $name="sync_timestamp_seconds", $help_text="Last main-loop metric sync, Unix time."]));
global peer_info = Telemetry::register_gauge_family([
    $prefix="forcad_game", $unit="", $name="peer_info", $help_text="Configured VPN peer to source team mapping.",
    $label_names=vector("source_ip", "source_team_ip")]);

type Flow: record {
    labels: vector of string;
    tcp: bool &default=F;
    ws: bool &default=F;
    udp: bool &default=F;
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
        }
    for ( source in sources )
        for ( target in vulnboxes )
            {
            local labels = vector(cat(source), cat(target));
            local counters = vector(http_requests, ws_upgrades, tcp_flows, tcp_established, udp_flows, gaps);
            for ( i in counters )
                Telemetry::counter_family_inc(counters[i], labels, 0.0);
            local gauges = vector(tcp_active, ws_active, udp_active);
            for ( j in gauges )
                Telemetry::gauge_family_set(gauges[j], labels, 0.0);
            }
    }

event new_connection(c: connection)
    {
    if ( c$id$orig_h !in peers || c$id$resp_h !in vulnboxes || c$id$resp_p !in game_ports )
        return;
    c$forcad = [$labels=vector(cat(peers[c$id$orig_h]), cat(c$id$resp_h))];
    if ( get_port_transport_proto(c$id$resp_p) == tcp )
        Telemetry::counter_family_inc(tcp_flows, c$forcad$labels);
    else if ( get_port_transport_proto(c$id$resp_p) == udp )
        {
        c$forcad$udp = T;
        Telemetry::counter_family_inc(udp_flows, c$forcad$labels);
        Telemetry::gauge_family_inc(udp_active, c$forcad$labels);
        }
    }

event connection_established(c: connection)
    {
    if ( ! c?$forcad || c$forcad$tcp )
        return;
    c$forcad$tcp = T;
    Telemetry::counter_family_inc(tcp_established, c$forcad$labels);
    Telemetry::gauge_family_inc(tcp_active, c$forcad$labels);
    }

event http_request(c: connection, method: string, original_URI: string, unescaped_URI: string, version: string)
    {
    if ( c?$forcad )
        Telemetry::counter_family_inc(http_requests, c$forcad$labels);
    }

event http_connection_upgrade(c: connection, protocol: string)
    {
    if ( ! c?$forcad || c$forcad$ws || to_lower(protocol) != "websocket" )
        return;
    c$forcad$ws = T;
    Telemetry::counter_family_inc(ws_upgrades, c$forcad$labels);
    Telemetry::gauge_family_inc(ws_active, c$forcad$labels);
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

event connection_finished(c: connection) { close_flow(c); }
event connection_reset(c: connection) { close_flow(c); }
event connection_state_remove(c: connection) { close_flow(c); }

event content_gap(c: connection, is_orig: bool, seq: count, length: count)
    {
    if ( c?$forcad )
        Telemetry::counter_family_inc(gaps, c$forcad$labels);
    }

hook Telemetry::sync()
    {
    local stats = get_net_stats();
    Telemetry::counter_set(captured, count_to_double(stats$pkts_recvd));
    Telemetry::counter_set(dropped, count_to_double(stats$pkts_dropped));
    Telemetry::gauge_set(heartbeat, time_to_double(current_time()));
    }
