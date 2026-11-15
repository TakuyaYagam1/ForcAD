# Local network frontend demo

This runner serves only the production frontend build made with Vite's `demo`
mode. It does not start Docker, Vite's development server, an API proxy, or any
backend process. Requests under `/api` return 404. The Python server serves
files from `frontend/dist` only and falls back to `index.html` for client-side
routes.

## Build and start

Run from the repository root after the UI changes are ready:

```sh
scripts/local-demo.sh build
scripts/local-demo.sh start
scripts/local-demo.sh status
```

The script binds to the current `en0` IPv4 address and TCP port 5173. Devices
on the same network can open the URL printed by `start` or `status`.
If `en0` has no IPv4 address, set
`LOCAL_DEMO_HOST` explicitly. Use `LOCAL_DEMO_HOST=0.0.0.0` only when binding
all local interfaces is intentional. Override the port with
`LOCAL_DEMO_PORT=5173`. When overriding the port, pass the same value to
`start`, `status`, and `stop` so the runner identifies the recorded process.

Stop with:

```sh
scripts/local-demo.sh stop
```

The detached process PID and log are stored under `output/local-demo/`. There
is no launch agent, boot service, firewall change, router change, UPnP mapping,
or public tunnel. If the selected port is occupied, start fails and leaves the
existing listener untouched.

## Browser compatibility

Demo UUIDs use a cryptographically secure RFC 4122 v4 implementation backed by
`crypto.getRandomValues`, so creation and logs work on LAN HTTP origins even
when the secure-context-only `crypto.randomUUID` method is unavailable.

The demo clock advances rounds by their actual elapsed duration. Captures update
scores automatically every 12 seconds while the game is running and the document
is visible; every third capture can reorder the leading teams. Captures do not
reset the current round's progress. There is no demo control strip. The normal
admin interface is available at `/admin/login` with `demo` / `demo`.

The compact ASCII track in the shared header shows elapsed round progress and
the time remaining. Score changes appear immediately without a square-wave effect.
Demo data and admin edits remain per-browser in memory, resetting on a full reload.
