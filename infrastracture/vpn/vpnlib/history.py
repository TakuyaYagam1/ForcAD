"""Read WireGuard endpoint observations over a judge's SSH-forwarded Unix socket."""

import html
import ipaddress
import os
import re
import socket
import sqlite3
import sys
import time
from contextlib import closing
from datetime import datetime, timezone
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler
from pathlib import Path
from socketserver import UnixStreamServer
from typing import cast
from urllib.parse import parse_qsl, urlencode, urlsplit

LIMIT = 200


def _number(text: str) -> int:
    if not re.fullmatch(r"[0-9]{1,15}", text):
        raise ValueError("Invalid time or cursor")
    return int(text)


def _utc(seconds: int) -> str:
    return datetime.fromtimestamp(seconds, timezone.utc).strftime("%Y-%m-%d %H:%M:%S")


def _link(fields: dict, label: str) -> str:
    return f'<a href="?{html.escape(urlencode(fields))}">{html.escape(label)}</a>'


def page(database: Path, query: str) -> str:
    fields = parse_qsl(query, keep_blank_values=True, strict_parsing=True, max_num_fields=5)
    args = dict(fields)
    if len(args) != len(fields) or set(args) - {"peer", "from", "to", "before", "row"}:
        raise ValueError("Unknown or repeated filter")
    until = _number(args.get("to", str(time.time_ns() // 1_000_000)))
    since = _number(args.get("from", str(max(0, until - 6 * 3_600_000))))
    if since > until or until >= 253402300800000:
        raise ValueError("Invalid time range")
    # Events have whole-second resolution. Do not pull an event preceding 'from'
    # into the evidence window solely because Grafana uses milliseconds.
    start, end = (since + 999) // 1000, until // 1000
    scope = {"from": since, "to": until}
    if ("row" in args) != ("before" in args) or ("row" in args and "peer" not in args):
        raise ValueError("Invalid paging cursor")
    peer = str(ipaddress.IPv4Address(args["peer"])) if "peer" in args else None
    result = [
        '<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">',
        '<title>WireGuard endpoint history</title><style>html{color-scheme:light dark}body{font:16px system-ui;margin:2rem auto;padding:0 1rem;max-width:72rem}table{border-collapse:collapse}th,td{text-align:left;padding:.4rem .7rem;border-bottom:1px solid GrayText}caption{text-align:left}p{max-width:70ch}</style><main>',
        '<h1>WireGuard endpoint history</h1>',
        f'<p>{_utc(since // 1000)} to {_utc(end)} UTC. Times are observations, not login times. A baseline can predate the event. Brief changes between polls can be missed.</p>',
    ]
    def cells(row):
        return '<tr>' + ''.join(f'<td>{html.escape(str(v))}</td>' for v in row) + '</tr>'

    with closing(sqlite3.connect(database.as_uri() + '?mode=ro', uri=True, timeout=2)) as db:
        if peer is None:
            result.append('<table><caption>Distinct IPs per team in this interval</caption><tr><th>Team</th><th>Profiles observed</th><th>Distinct external IPs</th><th>Distinct outside venue</th></tr>')
            for row in db.execute('''SELECT team_name,count(DISTINCT source_ip),count(DISTINCT external_ip),
                    count(DISTINCT CASE WHEN venue='outside' THEN external_ip END) FROM observations
                    WHERE observed_at BETWEEN ? AND ? GROUP BY team_name ORDER BY team_name''', (start, end)):
                result.append(cells(row))
            result.append('</table><p>One NAT address can serve several members. Distinct addresses are not a headcount.</p>')
            result.append('<table><caption>Profiles in retained history as of the end of the interval</caption><tr><th>Team</th><th>Profile</th><th>WG IP</th><th>Distinct IPs in interval</th><th>Outside observations in interval</th><th>Last observation UTC</th></tr>')
            rows = db.execute('''SELECT team_name,member,source_ip,count(DISTINCT CASE WHEN observed_at>=? THEN external_ip END),
                sum(observed_at>=? AND venue='outside'),max(observed_at) FROM observations
                WHERE observed_at<=? GROUP BY team_name,member,source_ip ORDER BY team_name,member''', (start, start, end))
            for team, member, addr, count, outside, latest in rows:
                result.append(f'<tr><td>{html.escape(team)}</td><td>{html.escape(member)}</td><td>' + _link({**scope, 'peer': addr}, addr) + f'</td><td>{count}</td><td>{outside}</td><td>{_utc(latest)}</td></tr>')
            result.append('</table><p>An empty interval means no logged changes, not an inactive peer. Open a profile to see its last observation before the interval.</p>')
        else:
            result.append('<p>' + _link(scope, 'All profiles') + f'</p><h2>{peer}</h2>')
            previous = db.execute('''SELECT observed_at,team_name,member,external_ip,venue,kind FROM observations
                WHERE source_ip=? AND observed_at<? ORDER BY observed_at DESC,id DESC LIMIT 1''', (peer, start)).fetchone()
            if previous:
                ts, team, member, addr, venue, kind = previous
                result.append('<p>Last observation before this interval (not evidence of activity within it): ' + html.escape(f'{_utc(ts)} UTC | {team} / {member} | {addr} | {venue} | {kind}') + '</p>')
            sql = '''SELECT id,observed_at,team_name,member,external_ip,venue,kind FROM observations
                WHERE source_ip=? AND observed_at BETWEEN ? AND ?'''
            params = [peer, start, end]
            if 'row' in args:
                sql += ' AND (observed_at,id)<(?,?)'
                params.extend((_number(args['before']), _number(args['row'])))
            rows = db.execute(sql + ' ORDER BY observed_at DESC,id DESC LIMIT ?', [*params, LIMIT + 1]).fetchall()
            if rows:
                result.append('<table><tr><th>Observed UTC</th><th>Team</th><th>Profile</th><th>External IP</th><th>Venue</th><th>Observation</th></tr>')
                for _, ts, *data in rows[:LIMIT]:
                    result.append(cells((_utc(ts), *data)))
                result.append('</table>')
                if len(rows) > LIMIT:
                    last_id, last_ts = rows[LIMIT - 1][:2]
                    result.append('<p>' + _link({**scope, 'peer': peer, 'before': last_ts, 'row': last_id}, 'Older observations') + '</p>')
            else:
                result.append('<p>No observations in the selected interval.</p>')
    return ''.join(result) + '</main></html>'


class HistoryServer(UnixStreamServer):
    database: Path


class Handler(BaseHTTPRequestHandler):
    timeout = 5

    def log_message(self, format, *args):
        # Neither peer addresses nor the request URL belong in the service journal.
        pass

    def do_GET(self):
        host = self.headers.get_all('Host', [])
        if len(host) != 1 or not re.fullmatch(r'(?:127\.0\.0\.1|localhost)(?::[0-9]{1,5})?', host[0]):
            self.send_error(HTTPStatus.FORBIDDEN)
            return
        try:
            url = urlsplit(self.path)
            if url.path != '/' or url.scheme or url.netloc:
                self.send_error(HTTPStatus.NOT_FOUND)
                return
            body = page(cast(HistoryServer, self.server).database, url.query).encode()
        except (ValueError, OverflowError):
            self.send_error(HTTPStatus.BAD_REQUEST, 'Invalid query')
            return
        except (sqlite3.Error, OSError):
            self.send_error(HTTPStatus.SERVICE_UNAVAILABLE, 'History unavailable; check the endpoint collector')
            return
        self.send_response(HTTPStatus.OK)
        self.send_header('Content-Type', 'text/html; charset=utf-8')
        self.send_header('Content-Length', str(len(body)))
        self.send_header('Cache-Control', 'no-store')
        self.send_header('Referrer-Policy', 'no-referrer')
        self.send_header('X-Content-Type-Options', 'nosniff')
        self.send_header('Cross-Origin-Resource-Policy', 'same-origin')
        self.send_header('Content-Security-Policy', "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'")
        self.end_headers()
        self.wfile.write(body)


def main():
    if len(sys.argv) != 2 or os.environ.get('LISTEN_PID') != str(os.getpid()) or os.environ.get('LISTEN_FDS') != '1':
        raise SystemExit('Requires a single socket-activated Unix listener and a database path')
    with HistoryServer('', Handler, bind_and_activate=False) as server:
        server.socket.close()
        server.socket = socket.socket(fileno=3)
        if server.socket.family != socket.AF_UNIX or server.socket.type != socket.SOCK_STREAM:
            raise SystemExit('Refusing a non-Unix listener')
        server.database = Path(sys.argv[1])
        server.serve_forever()


if __name__ == '__main__':
    main()
