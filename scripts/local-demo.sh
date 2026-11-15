#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER="$ROOT/scripts/local_demo_server.py"
DIST="$ROOT/frontend/dist"
STATE_DIR="$ROOT/output/local-demo"
PID_FILE="$STATE_DIR/server.pid"
LOG_FILE="$STATE_DIR/server.log"
PORT="${LOCAL_DEMO_PORT:-5173}"

usage() {
  cat <<'EOF'
Usage: scripts/local-demo.sh {build|start|stop|status}

  build  Build the frontend in demo mode (run only when the source is ready)
  start  Serve frontend/dist on the current en0 IPv4 address, port 5173
  stop   Stop only the server process recorded by this script
  status Show build, process, address, and HTTP status

Overrides: LOCAL_DEMO_HOST, LOCAL_DEMO_PORT
EOF
}

resolve_host() {
  if [[ -n "${LOCAL_DEMO_HOST:-}" ]]; then
    printf '%s' "$LOCAL_DEMO_HOST"
    return
  fi
  if command -v ipconfig >/dev/null 2>&1; then
    ipconfig getifaddr en0 2>/dev/null || true
  fi
}

is_our_process() {
  local pid="$1" command_line
  [[ "$pid" =~ ^[0-9]+$ ]] && (( pid > 1 )) || return 1
  kill -0 "$pid" 2>/dev/null || return 1
  command_line="$(ps -p "$pid" -o command= 2>/dev/null || true)"
  [[ "$command_line" == *"$SERVER"* && "$command_line" == *"--port $PORT"* ]]
}

read_pid() {
  [[ -f "$PID_FILE" ]] || return 1
  cat "$PID_FILE"
}

case "${1:-}" in
  build)
    cd "$ROOT/frontend"
    pnpm exec tsc -b
    pnpm exec vite build --mode demo
    ;;
  start)
    mkdir -p "$STATE_DIR"
    HOST="$(resolve_host)"
    if [[ -z "$HOST" ]]; then
      echo "Could not resolve en0 IPv4. Set LOCAL_DEMO_HOST explicitly." >&2
      exit 1
    fi
    if [[ ! -f "$DIST/index.html" ]]; then
      echo "Demo build missing: $DIST/index.html" >&2
      echo "Run scripts/local-demo.sh build after source changes are ready." >&2
      exit 1
    fi
    if [[ -f "$PID_FILE" ]]; then
      PID="$(read_pid)"
      if is_our_process "$PID"; then
        echo "Local demo already running as PID $PID at http://$HOST:$PORT/"
        exit 0
      fi
      rm -f "$PID_FILE"
    fi
    if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then
      echo "Port $PORT already has a listener; leaving it untouched." >&2
      lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >&2 || true
      exit 1
    fi
    PID="$(python3 - "$SERVER" "$DIST" "$HOST" "$PORT" "$LOG_FILE" <<'PYTHON'
import subprocess
import sys

server_path, build_path, bind_host, bind_port, log_path = sys.argv[1:]
with open(log_path, "ab", buffering=0) as log:
    process = subprocess.Popen(
        [sys.executable, server_path, "--root", build_path, "--host", bind_host, "--port", bind_port],
        stdin=subprocess.DEVNULL,
        stdout=log,
        stderr=subprocess.STDOUT,
        start_new_session=True,
    )
print(process.pid)
PYTHON
)"
    printf '%s\n' "$PID" >"$PID_FILE"
    sleep 1
    if ! is_our_process "$PID"; then
      echo "Demo server did not stay running; see $LOG_FILE" >&2
      rm -f "$PID_FILE"
      exit 1
    fi
    echo "Local demo running: http://$HOST:$PORT/ (PID $PID)"
    echo "Log: $LOG_FILE"
    ;;
  stop)
    if [[ ! -f "$PID_FILE" ]]; then
      echo "Local demo is not running (no PID file)."
      exit 0
    fi
    PID="$(read_pid)"
    if ! is_our_process "$PID"; then
      echo "Recorded PID is not this demo server; leaving process untouched."
      rm -f "$PID_FILE"
      exit 0
    fi
    kill "$PID"
    for _ in {1..30}; do
      kill -0 "$PID" 2>/dev/null || break
      sleep 0.1
    done
    if kill -0 "$PID" 2>/dev/null; then
      echo "Server did not stop after SIGTERM; not sending further signals." >&2
      exit 1
    fi
    rm -f "$PID_FILE"
    echo "Local demo stopped."
    ;;
  status)
    HOST="$(resolve_host)"
    printf 'Build: '
    [[ -f "$DIST/index.html" ]] && echo "ready ($DIST)" || echo "not built"
    if [[ -f "$PID_FILE" ]]; then
      PID="$(read_pid)"
      if is_our_process "$PID"; then
        echo "Process: running (PID $PID)"
        echo "URL: http://${HOST:-<en0-ip>}:$PORT/"
        if command -v curl >/dev/null 2>&1 && [[ -n "$HOST" ]]; then
          curl --silent --output /dev/null --write-out 'HTTP: %{http_code}\n' \
            --max-time 2 "http://$HOST:$PORT/" || true
        fi
      else
        echo "Process: stopped or PID reused (stale PID file)"
      fi
    else
      echo "Process: stopped"
    fi
    [[ -f "$LOG_FILE" ]] && echo "Log: $LOG_FILE"
    ;;
  *) usage; exit 2 ;;
esac
