![tests](https://github.com/pomo-mondreganto/ForcAD/workflows/tests/badge.svg)

# ForcAD

Attack-Defence CTF platform with Python checkers, a React scoreboard and an admin panel.

The name is pronounced as "forkád".

> This documentation is for the latest (development) version of ForcAD. It might not be stable or even working. The latest stable version can be found [here](https://github.com/pomo-mondreganto/ForcAD/releases/latest), see the README.md there.

Note that there's a [wiki](https://github.com/pomo-mondreganto/ForcAD/wiki) containing some useful queries for game
statistics, services description, writing a checker, modifying the rating system, etc.

## Table of contents

<!-- toc -->

* [Running](#running)
    * [Frontend theme](#frontend-theme)
    * [Existing databases](#existing-databases)
    * [Reset and cleanup](#reset-and-cleanup)

* [Configuration and usage](#configuration-and-usage)
    * [Receiving flags](#receiving-flags)
    * [Flag format](#flag-format)
    * [Configuration file](#configuration-file)
    * [Team logos](#team-logos)
    * [Applying a round limit to an existing game](#applying-a-round-limit-to-an-existing-game)
    * [Starting a rehearsal or final from the admin page](#starting-a-rehearsal-or-final-from-the-admin-page)

* [Checkers](#checkers)
    * [Configuration](#configuration)
    * [Checkers folder](#checkers-folder)
    * [Writing a checker](#writing-a-checker)

* [Wiki](#wiki)

<!-- tocstop -->

## Running

Run these commands from the project root. You need Docker with the Compose plugin,
uv and Make. Local Python installation also needs a C compiler and PostgreSQL client
headers with `pg_config` for `psycopg2`.

1. Copy `config/config.yml.example` to `config/config.yml`.
2. Configure teams, tasks, `start_time`, `timezone`, `round_time` and `rounds`.
   Put executable checker programs under `checkers/`.
3. Run `make sync` to install the Python project and operator tools.
4. Run `make setup` to validate the configuration and create runtime settings in
   `config/runtime/`. Missing admin and storage credentials are generated separately
   and saved to private files. Existing credentials are preserved; passwords are not
   printed. Setup also creates a backup beside the game configuration.
5. Run `make up` to build missing images and start the application.

The scoreboard is at `http://127.0.0.1:8080/`, the admin panel at `/admin/`, and
Flower at `/flower/`. `make up` does not run setup or reset game data. Use
`make build` after changing source code or checker dependencies, then `make up`.

Compose files live in `deployment/docker/`; the CLI selects them explicitly:

```bash
uv run --project backend --locked --group cli forcad rd ps
uv run --project backend --locked --group cli forcad rd logs --tail 100 http-receiver
```

`./control.py` remains a compatibility wrapper for the installed CLI. Its `--fast`
option is accepted for existing commands and uses the same Compose configuration.
`make dev` adds a backend source mount. To run the frontend development server,
use `pnpm --dir frontend install --frozen-lockfile` and `pnpm --dir frontend dev`.

### Frontend theme

The frontend theme is compiled into the nginx image. These values are public and
must not contain secrets. Empty values use the defaults; the frontend validates
the two color values.

```bash
export VITE_BRAND_NAME='ForcAD'
export VITE_BRAND_LOGO='/brand/forcad-mark.svg'
export VITE_BRAND_PRIMARY='#5B8DEF'
export VITE_BRAND_ACCENT='#38B7B0'
./control.py rd build nginx
```

Set `VITE_BRAND_LOGO` to an asset path present under `public/`, such as
`/brand/forcad-mark.svg`. Rebuilding
nginx compiles the values into the frontend. Changing nginx runtime environment
variables does not change the JavaScript bundle.

### Existing databases

The initializer creates the schema for a fresh database. The additive update in
`backend/src/forcad/scripts/apply_fixes.py` has been integration-tested against the main
database fixture for revision `16b0842`. Treat that fixture as the verified source
schema; compatibility with arbitrary older main schemas has not been established.

The update assumes `Teams.token` already exists and that every team row has a
non-null, unique token containing exactly 16 lowercase hexadecimal characters.
It validates tokens without changing them and stops before schema changes if the
column is missing or any token is invalid or duplicated. The tested game
configuration also has a finite `game_hardness` greater than 1. The update adds or
adjusts schema fields, constraints, and dispatch/session database objects; it does
not import data or regenerate tokens.

The migration takes an `ACCESS EXCLUSIVE` lock on `Teams` while it validates and
updates that table. Pause the game and stop application writers before running it.
Back up PostgreSQL, verify that the backup restores, and apply the update to a
restored staging copy first. Do not point a new build at an existing production
database until its schema and migration have been reviewed.

For a local migration check, use a separate checkout, a local `config/config.yml` based
on `config/config.yml.example`, and a restored staging database. Run `make test` and
`make -C security test` for the offline checks; they do not start the application
or production services. To keep existing game data, back it up, verify a restore,
and review a migration for the actual schema before reusing the database.

The initializer leaves an existing database unchanged. Editing the YAML and
restarting containers does not reapply game settings, teams or tasks.

### Reset and cleanup

These commands delete game data. Back up a game you need to retain before running them.

| Command | Result | Next step |
| --- | --- | --- |
| `./control.py reset` | Stops game services, clears game tables and Redis, removes containers and Compose volumes. Keeps the PostgreSQL cluster and its credentials. | `./control.py start` |
| `./control.py reset --full` | Removes the local PostgreSQL cluster as well, including its old credentials. Keeps `config/config.yml`, generated environment files and checkers. | `./control.py start` |
| `./control.py clean` | Performs a full local reset, then removes generated environment files and `config/runtime/compose.external.yml`. Keeps `config/config.yml`, canonical Compose files and checkers. | `./control.py setup`, then `./control.py start` |

Cleanup preserves Docker images and build cache.
Configured team tokens remain in `config/config.yml` and are reused on initialization. Teams without configured tokens
receive new random tokens.

When changing the database credentials for a new game:

```bash
./control.py reset --full
# Edit storage credentials and game settings in config/config.yml.
./control.py setup
./control.py start
```

Full cleanup also works when `setup` has already changed the password and the old database rejects connections:
it does not log in to PostgreSQL. It uses the cached PostgreSQL image to remove the container-owned data, so no
manual `sudo rm` is needed. Full cleanup requires a local Unix-socket Docker daemon supporting
`bind-recursive=disabled` and the standard `docker_volumes/postgres/data` bind mount.
External storages and custom PostgreSQL mounts are supported by ordinary `reset`, but not by `reset --full` or
`clean`. Those commands refuse unsupported storage layouts before stopping services.

Cleanup refuses symlinked storage paths and unrecognized PostgreSQL data layouts.
It leaves unrelated files in `docker_volumes` and empty parent directories in place. Repeating `clean` after a
successful cleanup is safe. If generated configuration is missing while game data or containers remain, run
`setup` to restore it before cleanup.

Failures return a nonzero exit status and do not print a success message. If ordinary `reset` fails, the game
services remain stopped for diagnosis. Storage connection retries and SQL execution have time limits.

## Configuration and usage

### Receiving flags

Teams submit a JSON array of up to 100 flags to `PUT /flags`, with their token in
the `X-Team-Token` header. The response contains a result for each flag; an HTTP
200 response alone does not mean every flag was accepted.

Set `teams[].token` to retain a token after reset, or omit it to generate one on
database initialization. After startup, use `./control.py print_tokens` to retrieve
the tokens. Distribute only each team's own token.

### Flag format

System uses the most common flag format by default: `[A-Z0-9]{31}=`, the first symbol is the first letter of
corresponding service name. You can change flag generation in function `Flag.generate` in
[backend/src/forcad/lib/models/flag.py](backend/src/forcad/lib/models/flag.py).

Each flag is valid (received by flag receivers and can be checked by checker) for `flag_lifetime` rounds (game config
variable).

### Configuration file

The configuration in `config/config.yml` has five sections. Start with
[config/config.yml.example](config/config.yml.example); `admin` and `storages` can
be omitted before setup.

* **game** contains the following settings:

    * `mode` (optional, default `classic`): `classic` waits for round checks before
      advancing; `blitz` schedules task checks separately.

    * `start_time` (required): scheduled final start in the configured `timezone`.
      Use an unquoted YAML datetime without a UTC offset, for example
      `2027-01-01 12:00:00`.

    * `round_time` (required): round duration in seconds. Example: `30`.

    * `get_period` (optional, defaults to `round_time`): interval in seconds between
      GET schedules in `blitz` mode. A task can override this value.

    * `rounds` (optional, default unlimited): positive integer limiting the number of playable rounds.
      With `rounds: 300` and `round_time: 60`, the game finishes at the boundary after round 300,
      without starting round 301. This is five hours without pauses or checker delays.
      Pauses preserve the remaining round time; unfinished checks can delay earlier round transitions.
      At the final boundary, flag reception and new checks stop. Already sent checks can finish
      before the final scoreboard is published. The limit applies to scheduled games, manually
      started finals and rehearsals. Each new session starts its round count from one;
      rehearsals retain their cleanup behavior.
      Omit `rounds` or set it to `null` to disable this automatic limit.

    * `flag_lifetime` (required): flag lifetime in rounds (see [flag format](#flag-format) section). Example: `5`.

    * `timezone` (optional, default `UTC`): the timezone in which `start_time` is specified. Example: `Europe/Moscow`.

    * `default_score` (optional, default `2500`): default score for tasks.

    * `env_path` (optional): path to prepend to the checker's `$PATH` environment
      variable. Use `/opt/checkers/bin` for the Python environment installed from
      `checkers/requirements.txt`. A task can override it.

    * `game_hardness` (optional, default `10`): finite game hardness parameter greater than 1
      (see [rating system](https://github.com/pomo-mondreganto/ForcAD/wiki/Rating-system) wiki page). Example: `10.5`.

    * `inflation` (optional, default `true`): inflation
      (see [rating system](https://github.com/pomo-mondreganto/ForcAD/wiki/Rating-system) wiki page). Example: `true`.

    * `checkers_path` (optional, default `/checkers/`): base directory inside the worker
      container. Compose mounts the host's `checkers/` directory there.

    * `volga_attacks_mode` (optional, default `false`): accept submissions only while
      the attacker's corresponding service has status UP.

* **admin** contains credentials to access celery visualization (`/flower/` on scoreboard) and admin panel:

    * `username: forcad`
    * `password: **change_me**`

    When omitted, setup uses username `forcad` and generates a random password.
    Storage passwords are generated independently. Changing admin credentials
    does not replace existing storage credentials.

* **teams** contains playing teams. Example contents:

```yaml
teams:
  - ip: 10.70.0.2
    name: Team1
  - ip: 10.70.1.2
    name: "Team2 (highlighted)"
    highlighted: true
```

`ip` and `name` are required. `highlighted` defaults to `false` and marks a team
visually on the scoreboard. `logo_path` and `token` are optional.

### Team logos

Place local logo files in `public/teams/` and set `teams[].logo_path` to a path such
as `/team-logos/team1.svg`. Nginx serves this directory read-only. You can also
use a validated root-relative path or an HTTP(S) URL. The logo catalog may be
empty; teams without a logo use the default avatar. No team names or logos are
bound to a particular event.

Each team can optionally specify a fixed token:

```yaml
teams:
  - ip: 10.70.0.2
    name: Team1
    token: "0123456789abcdef"  # Example only; use your own random token.
  - ip: 10.70.1.2
    name: Team2
```

Tokens must be unique strings of exactly 16 lowercase hexadecimal characters (`0-9`, `a-f`).
Quote tokens in YAML, especially values containing only digits. `control.py setup` preserves configured tokens.
After `control.py reset` and the next start, Team1 receives its configured token and Team2 receives a new random
token. An omitted token or `token: null` uses the original random generation; generated tokens are not saved to
the YAML. Empty strings and invalid or duplicate tokens are rejected.

These settings apply when initializing a fresh database. Editing the YAML and restarting an already initialized
game does not change its stored tokens. Keep the configuration and its backups private, and distribute only each
team's own token.

* **tasks** contains configuration of checkers and task-related parameters. More detailed explanation is
  in [checkers](#checkers) section. Example:

```yaml
tasks:
  - checker: collacode/checker.py
    checker_type: pfr
    checker_timeout: 30
    default_score: 1500
    gets: 3
    name: collacode
    places: 1
    puts: 3

  - checker: tiktak/checker.py
    checker_type: hackerdom
    checker_timeout: 30
    gets: 2
    name: tiktak
    places: 3
    puts: 2
```

* **storages** contains connection settings for PostgreSQL, Redis and RabbitMQ.
  Setup generates this section when missing and preserves an existing section:

    * `db`: PostgreSQL settings:

        * `user: <admin.username>`
        * `password: <independent generated password>`
        * `dbname: forcad`
        * `host: postgres`
        * `port: 5432`

    * `redis`: Redis (cache) settings:

        * `password: <independent generated password>`
        * `db: 0`
        * `host: redis`
        * `port: 6379`

    * `rabbitmq`: RabbitMQ (broker) settings:

        * `user: <admin.username>`
        * `password: <independent generated password>`
        * `host: rabbitmq`
        * `port: 5672`
        * `vhost: forcad`

To use external storage, pass `--database HOST:PORT`, `--redis HOST:PORT` or
`--rabbitmq HOST:PORT` to `./control.py setup`. These options replace the selected
addresses and generate `config/runtime/compose.external.yml` without those local
storage containers. The canonical Compose file remains unchanged.

The configuration models are in
[backend/src/forcad/cli/models.py](backend/src/forcad/cli/models.py).
`BasicConfig` describes the input to setup; `Config` includes generated credentials
and storage settings.

### Applying a round limit to an existing game

The existing-database instructions in this section and below require the schema
prerequisites described in [Existing databases](#existing-databases).
Back up the database before applying a migration.

For a new database, `game.rounds` is loaded by the initializer. An existing database
keeps its stored configuration when containers restart. To apply the limit without
resetting teams, tokens, scores or history, edit `game.rounds` in `config/config.yml`, then
run these commands from the project directory with the database and Redis running:

```bash
./control.py build
./control.py rd run --rm --no-deps config-loader
./control.py rd run --rm --no-deps --entrypoint /opt/forcad/bin/python initializer \
  -m forcad.scripts.apply_fixes --config /run/forcad-config/config.yml
./control.py rd up -d
```

The migration applies only `game.rounds` from that file. It preserves `start_time`
and the current lifecycle; a finished official game stays finished. Missing or null
`rounds` removes the limit. Running `forcad.scripts.apply_fixes` without `--config` only
updates the schema and preserves the stored limit. A limit at or below the current
round finishes a running game at its next round boundary.

### Starting a rehearsal or final from the admin page

Before the scheduled start, `/admin/` offers two separate controls:

* **Начать тестовую игру** starts a rehearsal. **Завершить тестовую игру**, the round
  limit, or the configured start date ends it. After outstanding checks finish, test
  scores, flags and history are cleared. Teams, tokens and settings are kept.
* **Начать финал** starts the official game immediately, even when `start_time` is
  in the future. Finish the rehearsal and wait for cleanup before starting the final.
  The final starts at round one and uses the configured `rounds` limit independently
  of the rehearsal. **Завершить игру** or the round limit ends the final and preserves
  its results. A finished final cannot be resumed; use pause for a temporary break.

The manual final start is stored in PostgreSQL. Restarting containers or reaching
the configured start date does not reset or restart that final. If no final is
started manually, the existing scheduled start still applies. The legacy
`POST /api/admin/game/start/` retains its date-based behavior; the new controls use
`start_practice/` and `start_final/`. The latter requires `confirm: true`, as does
`finish/`. The UI sends the session generation to reject stale commands.

For an existing installation, build the updated images and apply the additive
session migration before recreating the services, with PostgreSQL and Redis running:

```bash
./control.py build
./control.py rd run --rm --no-deps --entrypoint /opt/forcad/bin/python initializer \
  -m forcad.scripts.apply_fixes
./control.py rd up -d
```

This migration preserves the stored configuration and all existing results. It does
not convert a running rehearsal into a final. For a new database, initialization
creates the required session fields automatically.

## Checkers

### Configuration

Checksystem is completely compatible with Hackerdom checkers, but some config-level enhancements were added (see below).
Checkers are configured for each task independently. It's recommended to put each checker in a separate folder
under `checkers` in project root. Checker is considered to consist of the main executable and some auxiliary files in
the same folder.

The following options are supported:

* `name` (required): name of the service shown on the scoreboard.

* `checker` (required): executable path relative to `game.checkers_path`.
  Checkers run as the container's unprivileged `forcad` user. Make the program and
  its parent directories readable and executable by that user.
  A typical path is `<service_name>/checker.py`.

* `checker_timeout` (optional, default `10`): timeout in seconds for **each** checker action. As there're at minumum 3 actions run
  (depending on `puts` and `gets`), it's recommended to set `round_time` at least 4 times greater than the maximum
  checker timeout if possible.

* `puts` (optional, default `1`): number of flags to put for each team for each round.

* `gets` (optional, default `1`): number of flags to check from the last `flag_lifetime` rounds
  (see [Configuration and usage](#configuration-and-usage) for lifetime description).

* `places` (optional, default `1`): large tasks may contain a lot of possible places for a flag, that is the number.
  It's randomized for each `put` in range `[1, places]` and passed to the checker's `PUT` and `GET` actions.

* `checker_type` (optional, default `hackerdom`): an option containing underscore-separated tags,
  (missing tags are ignored). Examples: `hackerdom` (hackerdom tag ignored, so no modifier tags are applied),
  `pfr` (checker with public flag data returned). Currently, supported tags are:

    * `pfr`: checker returns public flag data (e.g. username of flag user) from `PUT` action as a **public message**,
      private flag data (`flag_id`) as a **private message**, and **public message** is shown
      on `/api/client/attack_data` for participants. If checker does not have this tag, no attack data is shown for the
      task.

    * `nfr`: `flag_id` passed to `PUT` is also passed to `GET` the same flag. That way, `flag_id` is used to seed the
      random generator in checkers so it would return the same values for `GET` and `PUT`. Checkers supporting this
      options are quite rare (and old), so **don't use it** unless you're sure.

More detailed explanation of checker tags can be
found [in this issue](https://github.com/pomo-mondreganto/ForcAD/issues/18#issuecomment-618072993).

* `env_path` (optional): path or a combination of paths to be prepended to `PATH` env
  variable (e.g. path to chromedriver). Defaults to `game.env_path`.

* `default_score` (optional): initial service score, overriding `game.default_score`.

* `get_period` (optional): GET scheduling interval in seconds for `blitz`, overriding
  the game setting.

See more in [checker writing](#writing-a-checker) section.

### Checkers folder

`checkers` folder in project root (containing all checker folders) is recommended to have the following structure:

```yaml
checkers:
  - requirements.txt  # Shared checker dependencies; must be present.
  - task1:
      - checker.py    # Executable.
  - task2:
      - checker.py    # Executable.
```

The worker image installs these dependencies in `/opt/checkers`, separately from
the platform environment. Python checkers can keep their existing
`#!/usr/bin/env python3` entrypoint and CHECK/PUT/GET protocol.

### Writing a checker

See the [corresponding wiki page](https://github.com/pomo-mondreganto/ForcAD/wiki/Writing-a-checker) on how to write a
checker.

## Wiki

More extensive reading can be found in the [wiki pages](https://github.com/pomo-mondreganto/ForcAD/wiki).
