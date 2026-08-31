# MailPulse

Watches IMAP mailboxes and turns mail into notifications. A user connects a
mailbox, describes what they care about, and picks where to hear about it —
Telegram, email, SMS or a webhook.

The pipeline is: **sync → match → schedule → dispatch → deliver**. A worker
polls each due mailbox over IMAP from a UID cursor, evaluates every new message
against that user's filters, schedules an event run for each match, and a second
loop drains the queue and delivers. Repeat chains, acknowledgement and retries
live in the dispatcher, so a match can nag until someone acks it.

Go 1.25, Fiber, GORM, Postgres, Redis. Two binaries — `web` serves the API,
`worker` runs the loops — built from one wiring path so they cannot drift.

## Quick start

```sh
cp .env.example .env
# REQUIRED: the app refuses to start without a key
openssl rand -base64 32   # paste into SECURITY_ENCRYPTION_KEY
make up
```

That builds the stack, applies migrations and starts everything. Then:

```sh
make seed     # admin@gmail.com / 123
make logs     # tail web and worker
make ps       # service status
```

| | |
|---|---|
| API | http://localhost:3000/api |
| API docs (Swagger UI) | http://localhost:3000/api/docs |
| OpenAPI spec | http://localhost:3000/api/openapi.yaml |
| Adminer | http://localhost:8080 |
| Kafka UI | http://localhost:8081 |
| Worker health | http://localhost:3001/health |

`make help` lists every target.

The dev stack also runs [GreenMail](https://greenmail-mail-test.github.io/greenmail/),
a real IMAP + SMTP server with two mailboxes (`demo` and `ops`, password
`secret123`), so the mail client can be exercised without pointing at anyone's
actual inbox.

## Configuration

Entirely environment variables — there is no config file. Keys map by
upper-casing and replacing dots with underscores, so `database.host` reads
`DATABASE_HOST`. A `.env` is loaded into the environment when present, and real
environment variables always win over it, which is how production supplies
secrets without a file on disk. Defaults live in
[internal/config/viper.go](internal/config/viper.go) and keep the binaries
startable against services on localhost.

[.env.example](.env.example) documents every key. Two carry no default on
purpose:

- **`SECURITY_ENCRYPTION_KEY`** — 32 bytes base64. Encrypts mailbox credentials
  and notifier secrets with AES-GCM. A shipped default would read as protection
  while providing none, so an unset key stops the app.
- **OAuth client credentials** — a provider whose id and secret are unset is
  never registered, so `_authorize` answers 501 and tells the user to use an app
  password rather than sending them to a consent screen that will reject them.

## Layout

Clean architecture, dependencies pointing inward:

```
cmd/            web, worker, seeder, loadtest
internal/
  entity/       database rows
  model/        request and response shapes, plus converters
  repository/   data access
  usecase/      business logic, the pipeline, the dispatcher, the matcher
  delivery/     http handlers, routes, middleware; kafka consumers
  gateway/      outbound: imap, oauth, cache, notifier channels, secrets
  config/       wiring; Bootstrap builds the graph once at startup
db/             migrations (15 pairs, 16 tables) and seeds
api/            hand-written OpenAPI 3.1 spec, 75 operations
test/           integration and feature suites
```

Three things are plug-in registries — event handlers, notifier channels and mail
providers. Adding a provider like Fastmail is a seed row; adding a new *kind* of
provider is one interface implementation and one registration.

## Tests

Split by what a test needs to run, so `go test ./...` never needs a database.
Full detail in [test/README.md](test/README.md).

| Layer | Needs | Run |
|-------|-------|-----|
| unit | nothing | `make test-unit` |
| integration | postgres, redis | `make test-integration` |
| feature | the whole app, plus a mail server | `make test-feature` |

`make test` runs all three. Integration and feature are behind `integration` and
`feature` build tags and run inside the web container, where they reach Postgres
by service name.

## Load and benchmarks

`cmd/loadtest` holds three suites — `endpoints`, `worker` and `parser` — over a
shared runner. `make load` lists them; `make load-smoke` is the fast pass over
all three. Benchmarks sit beside the code they measure:

```sh
make bench > new.txt && benchstat old.txt new.txt
```

Compare two runs rather than reading absolute numbers, which mean nothing on
shared hardware.

## CI

[.github/workflows/ci.yml](.github/workflows/ci.yml) runs on pull requests and
on pushes to `main` and `dev`:

- **check** — gofmt, `go vet` under all three build-tag sets, and a `go mod tidy`
  diff
- **test** — all three layers against Postgres, Redis and GreenMail service
  containers, migrations applied first
- **image** — builds the `prod` target on every event, and pushes to
  `ghcr.io/matrosovdream/mailpulse-golang` from `main` and `v*` tags

One image carries both binaries; they differ only by entrypoint.

## Deployment

[docker-compose.prod.yml](docker-compose.prod.yml) runs compiled binaries with
no source mount, no admin UIs, and no database or broker port published to the
host. `web` speaks plain HTTP — put a TLS-terminating proxy in front of it.

Create `.env.prod` first. Compose refuses to start without these five:

```
DATABASE_USERNAME  DATABASE_PASSWORD  DATABASE_NAME  REDIS_PASSWORD  KAFKA_CLUSTER_ID
```

`SECURITY_ENCRYPTION_KEY` is required too, but it reaches the containers through
`env_file`, which compose never interpolates — so a missing key fails at app
startup rather than at `up`. Generate the cluster id once per environment:

```sh
docker run --rm apache/kafka:3.9.0 /opt/kafka/bin/kafka-storage.sh random-uuid
```

Then:

```sh
make prod-up
make prod-logs
```

Both services carry healthchecks. `web` is probed on `/api/health`, which pings
Postgres and Redis and answers 503 if either is down. `worker` serves no API, so
it has its own one-route listener on port 3001 reporting the same dependency
check plus whether each loop is still ticking — a process check would report a
wedged poller as healthy.

## Status

Working end to end and verified against a real IMAP server: auth and roles, the
full pipeline, generic IMAP with a UID cursor, credential re-checking, the
OAuth connect flow, and an API described by a hand-written spec.

Known gaps, so nobody has to discover them:

- Cron-expression recurrence is stored and validated but never expanded; only
  `repeat_interval_seconds` is honoured.
- Kafka is in the stack but carries nothing — the pipeline hands off through the
  database.
- No metrics. Worker capacity has been measured, but a running deployment cannot
  be watched yet.
- The OAuth flow has been exercised end to end against a fake provider, not a
  live consent screen; no application is registered with Google, Microsoft or
  Yandex.
- Password reset tokens are logged, not emailed.
