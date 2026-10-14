# Local demo

Two real notes containers, Python checkers, small flag farms, Prometheus and
Grafana. A disposable router counts traffic between the teams. This is a demo,
not a competition service: `/demo/flags` intentionally exposes stored flags.

Use a fresh local database and a free `172.30.240.0/24` Docker subnet. Keep
`tasks[].checker: examples/note/checker.py`, `places: 2` and
`checker_type: hackerdom` in your private `config/config.yml`. Configure teams:

```yaml
teams:
  - name: danosito's ai farm
    ip: 172.30.240.11
    logo_path: /team-logos/danosito.jpg
    traffic_cidrs: [172.30.240.11/32]
    token: null
  - name: pod egidoy
    ip: 172.30.240.12
    logo_path: /team-logos/egida.png
    traffic_cidrs: [172.30.240.12/32]
    token: null
```

From the repository root:

```sh
make -C backend sync-codegen
bash deployment/docker/scripts/demo.sh up
```

Open the board at <http://localhost:8080>, its admin UI at
<http://localhost:8080/admin/login>, and Grafana at
<http://localhost:3000/d/forcad-monitoring>. Prometheus is at
<http://localhost:9090>. Board credentials are in `config/runtime/admin.env`;
Grafana uses `admin` and the password from `config/runtime/demo.env`.

Start a practice game in the board admin UI. Allow two rounds and 30-60 seconds
for scrapes. Both services should become UP, scores should change, and the
dashboard should show per-team HTTP requests, flag submissions and network
rates. Farms submit one invalid value every five seconds so that the flags/s
graph stays visible between rounds. WebSocket metrics appear when a browser
opens the board; localhost spectators are included in overall metrics, not team rows.

The live event feed contains new steals received while the tab is connected.
Farms collect new flags once per round, so events arrive in short bursts.

```sh
bash deployment/docker/scripts/demo.sh logs -f demo-farm1 demo-farm2
bash deployment/docker/scripts/demo.sh stop
bash deployment/docker/scripts/demo.sh start
bash deployment/docker/scripts/demo.sh down
```

`stop`/`start` preserves notes. `down` removes the containers and their temporary
notes, but retains board/Grafana data. After `down` + `up`, reset the practice
session in the admin UI so old flags are not checked against empty services.
The script never resets an existing game or deletes volumes.

Only the demo overlay adds services, routes and router discovery. The main
Compose remains unchanged. NET_ADMIN applies to container namespaces only;
the notes process drops its setup capabilities before serving requests.
Farms share each team's network namespace, and peer routes pass through the
router without NAT. Board traffic goes directly to Nginx with the team IP.
Credentials stay in ignored `config/` files. YAML changes do not rewrite an
already initialized database.
