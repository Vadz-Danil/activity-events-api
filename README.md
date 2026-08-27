# activity-events-api

A service that records activity events, streams them back live and folds them into four-hour windows with a background
worker. Go API, PostgreSQL, React dashboard.

## Scope

The brief asked for four things, and those are the load-bearing parts of this repository:

| The brief                                       | Here                                                             |
|-------------------------------------------------|------------------------------------------------------------------|
| Accept creation of an activity event            | `POST /api/v1/events`, `/batch`                                  |
| Retrieve events filtered by user and date range | `GET /api/v1/events?user_id&from&to`                             |
| A job every 4 hours counting each user's events | [the aggregation job](#the-aggregation-job) → `activity_buckets` |
| Daily aggregated statistics                     | `GET /api/v1/stats/activity?granularity=day`                     |

Everything else is a deliberate addition, and each one exists to make a decision visible rather than to pad the diff:

* **Auth** (JWT with refresh rotation, optional Google sign-in) — the brief's event carries a
  `user_id` in its body. Taking it from a token instead is the difference between a demo and something you could point
  at the internet, and it is what makes "filtered by user" a permission question rather than a query parameter.
* **A live SSE feed** — the four-hour window is the whole point of the task, and it also means a fresh event is
  invisible for up to four hours. The feed shows the write path working instantly while the charts stay honest about
  their delay.
* **Idempotency keys, keyset pagination, advisory locks** — the parts of an ingest endpoint that only matter under
  concurrency, which is where interesting bugs live.
* **Deployment and CI** — so the thing can be opened rather than described.

If you only have ten minutes: [the aggregation job](#the-aggregation-job) and
[Decisions worth explaining](#decisions-worth-explaining) are where the reasoning is.

## Live

|                      |                                                          |
|----------------------|----------------------------------------------------------|
| Dashboard            | https://activity-events.pages.dev                        |
| API                  | https://api--activity-events--4jynm8rb8gw5.code.run      |
| API documentation    | https://api--activity-events--4jynm8rb8gw5.code.run/docs |
| Liveness / readiness | `/healthz` · `/readyz`                                   |

Demo account: **admin@example.com** / **admin123** — see [Demo accounts](#demo-accounts).

The API runs on Northflank, the database is Neon, the SPA is on Cloudflare Pages, and every push to
`main` goes through the pipeline in [docs/deploy.md](docs/deploy.md). Prometheus and Grafana run locally rather than on
the deployment — see [Monitoring](#monitoring).

## What it does

Events arrive over HTTP and are written synchronously to Postgres. The API immediately publishes each new event to any
open server-sent-event connection of that user. A separate worker process independently scans for four-hour windows that
have closed and recomputes them into a bucket table.

Nothing in the write path waits on the worker, and the worker never sees an individual request — the two sides are
coupled only through the database.

```
client → API → Postgres              (synchronous)
             → SSE broker            (same instant, into the feed)

worker → reads events → UPSERT into activity_buckets   (its own loop)
dashboard → reads activity_buckets                     (the charts)
```

A fresh event therefore shows up in the feed at once, and in the charts only after its window closes and the worker
recomputes it.

## Running with Docker

```bash
cp .env.example .env      # set JWT_SECRET, the rest has working defaults
make up                   # postgres, migrations, api, worker, frontend, prometheus, grafana
make seed                 # demo account + a week of events, already aggregated
```

|            |                                                                                       |
|------------|---------------------------------------------------------------------------------------|
| Dashboard  | http://localhost:5173                                                                 |
| API        | http://localhost:8080                                                                 |
| Prometheus | http://localhost:9090                                                                 |
| Loki       | http://localhost:3100 — queried through Grafana, no UI of its own                     |
| Grafana    | http://localhost:3000 — the dashboard is provisioned from `deploy/grafana/dashboards` |

## Demo accounts

Both environments carry **two** accounts on purpose: an admin to demonstrate the account picker,
`?user_id=` and the aggregation panel, and a plain one to show what a regular user is limited to.

| Where                | Sign in at                        | Account              | Password                                      | Role  |
|----------------------|-----------------------------------|----------------------|-----------------------------------------------|-------|
| Deployed             | https://activity-events.pages.dev | `admin@example.com`  | `admin123`                                    | admin |
| Deployed             | https://activity-events.pages.dev | `viewer@example.com` | `viewer123`                                   | user  |
| Local                | http://localhost:5173             | `demo@example.com`   | `demo-password-123`                           | admin |
| Local                | http://localhost:5173             | `viewer@example.com` | `viewer-password-123`                         | user  |
| Grafana (local only) | http://localhost:3000             | `admin`              | `admin` — `GRAFANA_USER` / `GRAFANA_PASSWORD` | —     |

`make seed` creates the local pair and fills a week of events for each; override the addresses and passwords with
`SEED_EMAIL`, `SEED_PASSWORD`, `SEED_ROLE`, `SEED_VIEWER_EMAIL` and
`SEED_VIEWER_PASSWORD`. The deployed pair was created the same way — see
[demo data](docs/deploy.md#demo-data) for seeding a deployment.

Prometheus and Loki have no login: they are reached through Grafana, which is where the access control sits. Both
datasources are provisioned, so nothing needs wiring by hand — open the *Activity Events* folder and the dashboard is
already there, graphs and logs together.

**Signed in as an admin you can:** pick any account from the dashboard's account selector (or type an id straight into
the field beside it), read the aggregation run history with its status and trigger, and recompute a closed window by
hand. Signed in as `viewer@example.com` none of that is there and `?user_id=` for anybody else answers `403` — the
quickest way to see both sides is to open the two accounts in two browser profiles.

Viewing another account shows their charts and the last 20 of their events. The live stream stays on your own account:
reading somebody's activity in real time is surveillance rather than reporting.

These credentials live here rather than on the sign-in form: the repository is public, and a pre-filled password on a
login screen is an invitation.

## Running on the host

Everything except the database can run outside Docker. `make infra` starts postgres, prometheus and grafana; the Go
process and the SPA run from source:

```bash
cp .env.example .env      # set JWT_SECRET
make infra                # postgres + monitoring in Docker

make migrate-up           # go run ./cmd/migrate up
go run ./cmd/app          # APP_MODE=all serves the API and runs the worker in one process
make seed                 # optional demo data

cd frontend && npm install && npm run dev
```

`APP_MODE` picks what the process does: `api` serves HTTP, `worker` only aggregates, `all` does both. The deployment
runs two containers from the same image; locally one process is simpler.

## Sample requests

```bash
API=http://localhost:8080

# Sign up (or /login) and keep the access token
TOKEN=$(curl -s -X POST $API/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"super-secret"}' | jq -r .access_token)
```

**Record an event.** `user_id` is taken from the token, never from the body, so no account can write events as another.
The brief's `action` and `metadata` are this API's `type` and `payload`.

```bash
curl -X POST $API/api/v1/events \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: checkout-42' \
  -d '{"type":"page_view","payload":{"page":"/home"}}'
```

```json
{
  "id": 707,
  "type": "page_view",
  "payload": {
    "page": "/home"
  },
  "occurred_at": "2026-08-27T07:17:36.777106Z",
  "created_at": "2026-08-27T07:17:36.810757Z",
  "idempotency_key": "checkout-42"
}
```

Sending that request again answers `200` with the same `id` instead of `201`, and no second row is written.`occurred_at`
is optional and defaults to now; a client may backdate it up to 90 days, and up to 5 minutes into the future to absorb
clock skew.

**Read the feed, filtered by date range.** Pages are keyset based: pass `next_cursor` back as
`cursor`. `type` may repeat to filter on several kinds at once.

```bash
curl -G $API/api/v1/events -H "Authorization: Bearer $TOKEN" \
  --data-urlencode 'from=2026-08-26T00:00:00Z' \
  --data-urlencode 'to=2026-08-27T00:00:00Z' \
  --data-urlencode 'type=page_view' \
  --data-urlencode 'limit=50'
```

```json
{
  "user_id": 2,
  "items": [
    {
      "id": 707,
      "type": "page_view",
      "payload": {
        "page": "/home"
      },
      "occurred_at": "2026-08-27T07:17:36.777106Z",
      "created_at": "2026-08-27T07:17:36.810757Z"
    }
  ],
  "next_cursor": "MTc1NjI4..."
}
```

**Read another user's feed.** `user_id` is refused with `403` unless the caller holds the `admin`
role; the same parameter works on `/stats/activity`.

```bash
curl -G $API/api/v1/events?user_id=42 -H "Authorization: Bearer $ADMIN_TOKEN"
```

**List the accounts worth looking at.** An admin panel that makes you guess numeric ids is not a panel, so the dashboard
fills its account picker from here. The counts come from `activity_buckets`
rather than a scan over `events`, which also means they exclude the window that is still open.

```bash
curl $API/api/v1/admin/users -H "Authorization: Bearer $ADMIN_TOKEN"
```

```json
{
  "items": [
    {
      "id": 64,
      "email": "demo@example.com",
      "role": "admin",
      "event_count": 1844,
      "last_event_at": "2026-08-27T07:58:11.204Z"
    },
    {
      "id": 104,
      "email": "viewer@example.com",
      "role": "user",
      "event_count": 180,
      "last_event_at": "2026-08-27T07:59:02.117Z"
    }
  ]
}
```

The response is a purpose-built projection: no password hashes, no Google subject ids, nothing an account picker does
not need.

**Aggregated statistics.** Four-hour windows by default, a daily rollup with `granularity=day`, and
`from`/`to` to pick the range (a week back by default, 90 days at most).

```bash
curl $API/api/v1/stats/activity -H "Authorization: Bearer $TOKEN"
curl "$API/api/v1/stats/activity?granularity=day" -H "Authorization: Bearer $TOKEN"
```

```json
{
  "user_id": 2,
  "from": "2026-08-25T00:00:00Z",
  "to": "2026-08-27T08:00:00Z",
  "bucket": "4h0m0s",
  "buckets": [
    {
      "bucket_start": "2026-08-26T12:00:00Z",
      "bucket_end": "2026-08-26T16:00:00Z",
      "event_count": 55,
      "type_counts": {
        "page.view": 32,
        "button.click": 12,
        "search.query": 4,
        "form.submit": 3,
        "file.upload": 2,
        "session.start": 2
      },
      "first_event_at": "2026-08-26T12:01:44.201443Z",
      "last_event_at": "2026-08-26T15:57:02.201443Z"
    }
  ]
}
```

**Live events over SSE.** The connection stays open; a `: keep-alive` comment arrives every 20 seconds so idle proxies
do not close it.

```bash
curl -N $API/api/v1/events/stream -H "Authorization: Bearer $TOKEN"
```

```
id: 709
event: activity
data: {"id":709,"type":"page_view","payload":{"page":"/home"},"occurred_at":"2026-08-27T07:17:51.300537Z","created_at":"2026-08-27T07:17:51.316639Z"}
```

**Admin: recompute a window and read the run history.** Omitting `bucket_start` recomputes the last closed window; a
window that has not closed yet is refused with `bucket_not_closed`.

```bash
curl -X POST $API/api/v1/admin/aggregation/runs \
  -H "Authorization: Bearer $ADMIN_TOKEN" -H 'Content-Type: application/json' \
  -d '{"bucket_start":"2026-08-27T00:00:00Z"}'

curl $API/api/v1/admin/aggregation/runs -H "Authorization: Bearer $ADMIN_TOKEN"
```

```json
{
  "id": 53,
  "bucket_start": "2026-08-27T00:00:00Z",
  "bucket_end": "2026-08-27T04:00:00Z",
  "status": "succeeded",
  "trigger": "manual",
  "users_touched": 2,
  "started_at": "2026-08-27T07:18:38.735504Z",
  "finished_at": "2026-08-27T07:18:38.811201Z"
}
```

Errors share one envelope, so a client branches on `code` rather than on prose:

```json
{
  "error": {
    "code": "bucket_not_closed",
    "message": "This bucket has not closed yet, only a finished window can be aggregated"
  }
}
```

## API documentation

The full contract lives in [`api/openapi.yaml`](api/openapi.yaml) — 20 operations, every error code, every query
parameter, with the reasoning attached to the endpoints where it matters.

* Browse it on the deployment: **https://api--activity-events--4jynm8rb8gw5.code.run/docs**
* Or locally after `make up`: http://localhost:8080/docs
* The raw document is served at `/openapi.yaml` and opens in any editor offline

The spec is embedded in the binary rather than copied into the image as a file. The image carries only binaries and
migrations, so a loose file would need one more `COPY` and a path variable — another way to ship a deployment whose
`/docs` returns 404. It costs 27 KB.

`/docs` is served only when the process runs the API; the worker answers 404 there, as it does for every other route.

## API

| Method and path                                                  | Access | What it does                                                    |
|------------------------------------------------------------------|--------|-----------------------------------------------------------------|
| `GET /api/v1/auth/methods`                                       | open   | Which sign-in options this deployment has                       |
| `POST /api/v1/auth/register` · `/login`                          | open   | Password sign-up and sign-in                                    |
| `GET /api/v1/auth/google/start` · `/callback` · `POST /exchange` | open   | Google authorization-code flow with PKCE                        |
| `POST /api/v1/auth/refresh` · `/logout`                          | open   | Rotation and revocation                                         |
| `GET /api/v1/auth/me`                                            | token  | Current user                                                    |
| `POST /api/v1/events` · `/batch`                                 | token  | One event, or up to 100 in a transaction                        |
| `GET /api/v1/events`                                             | token  | Keyset feed: `type`, `from`, `to`, `limit`, `cursor`, `user_id` |
| `GET /api/v1/events/stream`                                      | token  | Live events over SSE                                            |
| `GET /api/v1/stats/activity`                                     | token  | Buckets, or a daily rollup with `granularity=day`; `user_id`    |
| `GET /api/v1/admin/users`                                        | admin  | Accounts to pick from, with their event counts                  |
| `POST /api/v1/admin/aggregation/runs`                            | admin  | Recompute one window                                            |
| `GET /api/v1/admin/aggregation/runs`                             | admin  | Run history                                                     |
| `GET /healthz` · `/readyz` · `/metrics`                          | open   | Operations                                                      |

Google sign-in is optional: without `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` and
`GOOGLE_REDIRECT_URL` the API reports `google: false` from `/auth/methods` and the SPA leaves the button out, rather
than offering one that can only fail.

**Reads are scoped to the caller unless the caller is an admin.** `GET /events` and
`GET /stats/activity` answer for the `user_id` in the token; passing `?user_id=` for somebody else needs the `admin`role
and is refused with `403` otherwise. The live stream has no such parameter — watching another account in real time is
surveillance, not reporting. Beyond reading, `admin` only grants control over the aggregation job.

## The aggregation job

**What it computes.** For every four-hour window that has closed, per user: how many events happened and a breakdown by
type, written into `activity_buckets` keyed by `(user_id, bucket_start)`. One SQL statement groups the whole window for
every user at once — the worker never iterates over events in Go. On the deployment a window takes about 75 ms.

**When it runs.** The worker wakes every `AGGREGATION_TICK` (5 minutes) and asks the database which windows are still
unaggregated. The watermark is `max(bucket_start) WHERE status = 'succeeded'` in
`aggregation_runs`, so the answer survives a restart: a worker that was down for a day comes back and backfills the six
windows it missed, up to `AGGREGATION_BACKFILL` per pass. A cron expression would say *when* to run, while the question
that matters is *what is still unaggregated*.

Waking every 5 minutes for a window that is 4 hours long looks wasteful, and 282 of those 288 daily passes do find
nothing. Each costs a single `SELECT max(...)`, which is cheaper than waiting another four hours because the one
scheduled moment happened to land during a redeploy.

**Daily numbers.** `granularity=day` rolls the same six buckets up with `jsonb_each_text`. There is no second pass over
`events`, so the four-hour and the daily view cannot disagree. The process refuses to start if `AGGREGATION_BUCKET` does
not divide 24 hours evenly, which would make that rollup a lie.

**Every attempt is recorded**, including failures: `aggregation_runs` keeps the window, the outcome (`succeeded` /
`failed` / `skipped`), what started it (`schedule` / `manual`), how many users were touched, the timings and the error
text. That table is both the watermark and the audit log, and it is what `GET /api/v1/admin/aggregation/runs` returns.

**Two processes cannot double-count.** Aggregation is a function, not a process: the worker calls it on a schedule and
the API calls it when an admin presses the button, so two callers really can meet on one window. Each run holds
`pg_try_advisory_xact_lock` for the duration of its transaction; the loser records `skipped` and moves on. The lock is
transaction-scoped rather than session-scoped because a pooler in transaction mode hands the underlying connection to
somebody else after every commit.

**A pass stops at the first window that did not succeed** instead of continuing. The watermark is the newest
*successful* run, so skipping ahead would strand the failed window behind it forever.

**An open window is refused.** `RunBucket` will not aggregate a window whose end is still in the future — a half-counted
bucket would look complete and nothing would ever revisit it.

**Backdating has a known limit.** An event inserted with an `occurred_at` inside a window that was already aggregated is
not picked up by the scheduler, because the watermark only moves forward. Recompute that window through the admin
endpoint; the write is an UPSERT, so replaying a window any number of times converges on the same row.

## Monitoring

Optional in the brief, and done for both halves of it — metrics and logs.

**Metrics.** `/metrics` exposes HTTP rate, latency and in-flight counts, aggregation runs by outcome, how long a window
takes, how far aggregation trails the present, new events versus idempotency-key repeats, and open SSE connections.
Prometheus scrapes both processes every 15 seconds. Nothing is pushed: the application only increments numbers in memory
and answers when asked.

Both processes export the same metric names, told apart by a `component` label — that is how a scheduled run and an
admin's manual recompute stay distinguishable in the same graph.

Counters live in process memory and reset when a process restarts. That is not a defect: the dashboard graphs `rate()`,
and Prometheus recognises a counter reset instead of drawing a negative spike. Only the gauges — aggregation lag, open
connections — are meaningful as absolute numbers.

**Logs.** The application writes structured JSON to stdout with a request id per request, and that is all it does — it
has no idea Loki exists. Grafana Alloy reads the container logs through the Docker API, lifts `level` out of the JSON
into a label and ships the lines to Loki; Grafana queries it as a second datasource. So
`{service="worker", level="error"}` filters on an index rather than scanning text.

Alloy is filtered to this compose project by label, so it never picks up unrelated containers on the machine. It is also
why `LOG_FORMAT` defaults to `json` here: parsing a human-readable log format back into fields is exactly what
structured logging exists to avoid. Set `LOG_FORMAT=console` if you would rather read logs in a terminal — you lose the
`level` label and keep everything else.

The fourteen dashboard panels in `deploy/grafana/dashboards` are provisioned from the repository rather than clicked
together by hand, and the last two put the logs on the same screen as the graphs:
a spike in 5xx and the lines that caused it, side by side.

Prometheus, Loki and Grafana are deliberately not deployed. They start locally with `make infra`, and
`deploy/prometheus/targets/` lets that same local Grafana scrape the deployment over HTTPS.

## Decisions worth explaining

**Idempotency lives in the database.** A partial unique index on `(user_id, idempotency_key)`
decides; the code only reads back the row a conflict points at. Checking first and inserting second would be a race two
concurrent requests could both win.

**Keyset pagination, not `OFFSET`.** The cursor carries `(occurred_at, id)` and the query compares tuples, so events
sharing a timestamp are neither lost nor repeated between pages.

**Refresh tokens rotate, and a reused one revokes its whole family.** Rotation and revocation take the same
transaction-scoped advisory lock on the family: a successor row inserted by a parallel rotation is invisible to the
revoker until commit, so locking the old row alone would leave a live token behind.

**The event stream is an in-process broker.** `LISTEN/NOTIFY` needs a session connection the pooler does not give us, an
always-open one would stop Neon from suspending, and its payload cap is smaller than an event may be. Publishing goes
through an interface, so a cross-process broker replaces one file once a second replica exists.

**Migrations are a deploy step, not application startup.** `cmd/migrate` is a separate binary and a separate pipeline
job, so replicas never migrate concurrently and a rollback does not drag the schema with it.

## Testing

```bash
make test               # unit tests with -race
make test-integration   # repository tests against a real PostgreSQL
```

The split is not academic: unit tests run against fakes and stayed green while an advisory lock was silently missing
from a commit. Anything whose behaviour lives in SQL — locks, transactions, unique indexes, races — is covered by the
integration tests instead.

`cd frontend && npm run shot` drives the running app through Playwright, captures the sign-in page, the dashboard in
both themes and a narrow viewport, and exits non-zero on any console or network error.
