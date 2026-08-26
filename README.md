# activity-events-api

A service that records activity events, streams them back live and folds them into four-hour
windows with a background worker. Go API, PostgreSQL, React dashboard.

## Live

| | |
| --- | --- |
| Dashboard | https://activity-events.pages.dev |
| API | https://api--activity-events--4jynm8rb8gw5.code.run |
| Liveness / readiness | `/healthz` · `/readyz` |

Demo account: **admin@example.com** / **admin123** — seeded with a week of events and the `admin`
role, so the aggregation panel is visible.

The API runs on Northflank, the database is Neon, the SPA is on Cloudflare Pages, and every push to
`main` goes through the pipeline in [docs/deploy.md](docs/deploy.md). Prometheus and Grafana are
deliberately **not** deployed — monitoring runs locally from `docker-compose.yaml`, because keeping
a Grafana instance alive on a free tier buys nothing that a local one does not show.

## What it does

Events arrive over HTTP and are written synchronously to Postgres. The API immediately publishes
each new event to any open server-sent-event connection of that user. A separate worker process
independently scans for four-hour windows that have closed and recomputes them into a bucket table.

Nothing in the write path waits on the worker, and the worker never sees an individual request —
the two sides are coupled only through the database.

```
client → API → Postgres              (synchronous)
             → SSE broker            (same instant, into the feed)

worker → reads events → UPSERT into activity_buckets   (its own loop)
dashboard → reads activity_buckets                     (the charts)
```

A fresh event therefore shows up in the feed at once, and in the charts only after its window closes
and the worker recomputes it.

## Running locally

```bash
cp .env.example .env      # set JWT_SECRET, the rest has working defaults
make up                   # postgres, migrations, api, worker, frontend, prometheus, grafana
make seed                 # demo account + a week of events, already aggregated
```

| | |
| --- | --- |
| Dashboard | http://localhost:5173 |
| API | http://localhost:8080 |
| Prometheus | http://localhost:9090 |
| Grafana | http://localhost:3000 — the dashboard is provisioned from `deploy/grafana/dashboards` |

Local demo account: `demo@example.com` / `demo-password-123`.

## API

| Method and path | Access | What it does |
| --- | --- | --- |
| `GET /api/v1/auth/methods` | open | Which sign-in options this deployment has |
| `POST /api/v1/auth/register` · `/login` | open | Password sign-up and sign-in |
| `GET /api/v1/auth/google/start` · `/callback` · `POST /exchange` | open | Google authorization-code flow with PKCE |
| `POST /api/v1/auth/refresh` · `/logout` | open | Rotation and revocation |
| `GET /api/v1/auth/me` | token | Current user |
| `POST /api/v1/events` · `/batch` | token | One event, or up to 100 in a transaction |
| `GET /api/v1/events` | token | Keyset feed: `type`, `from`, `to`, `limit`, `cursor` |
| `GET /api/v1/events/stream` | token | Live events over SSE |
| `GET /api/v1/stats/activity` | token | Own buckets, or a daily rollup with `granularity=day` |
| `POST /api/v1/admin/aggregation/runs` | admin | Recompute one window |
| `GET /api/v1/admin/aggregation/runs` | admin | Run history |
| `GET /healthz` · `/readyz` · `/metrics` | open | Operations |

Google sign-in is optional: without `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` and
`GOOGLE_REDIRECT_URL` the API reports `google: false` from `/auth/methods` and the SPA leaves the
button out, rather than offering one that can only fail.

**`admin` is not a superuser over data.** Feeds and statistics are always filtered by the `user_id`
in the token, and no endpoint exposes another account's events. The role only grants control over
the background aggregation job.

## Decisions worth explaining

**"Every four hours" versus "daily".** The brief asks for both. Daily numbers here are a rollup of
the same six buckets rather than a second count, so the two can never disagree. The configuration
refuses to start if `AGGREGATION_BUCKET` does not divide a day evenly.

**Idempotency lives in the database.** A partial unique index on `(user_id, idempotency_key)`
decides; the code only reads back the row a conflict points at. Checking first and inserting second
would be a race two concurrent requests could both win.

**Keyset pagination, not `OFFSET`.** The cursor carries `(occurred_at, id)` and the query compares
tuples, so events sharing a timestamp are neither lost nor repeated between pages.

**Refresh tokens rotate, and a reused one revokes its whole family.** Rotation and revocation take
the same transaction-scoped advisory lock on the family: a successor row inserted by a parallel
rotation is invisible to the revoker until commit, so locking the old row alone would leave a live
token behind.

**The scheduler polls instead of firing on the boundary.** Its watermark is the newest successful
run in `aggregation_runs`, so a restart backfills whatever it missed, and a pass stops at the first
window that did not succeed rather than jumping over a gap. A cron expression would express *when*,
while the real question is *what is still unaggregated*.

**The event stream is an in-process broker.** `LISTEN/NOTIFY` needs a session connection the pooler
does not give us, an always-open one would stop Neon from suspending, and its payload cap is smaller
than an event may be. Publishing goes through an interface, so a cross-process broker replaces one
file once a second replica exists.

**Migrations are a deploy step, not application startup.** `cmd/migrate` is a separate binary and a
separate pipeline job, so replicas never migrate concurrently and a rollback does not drag the schema
with it.

## Testing

```bash
make test               # unit tests with -race
make test-integration   # repository tests against a real PostgreSQL
```

The split is not academic: unit tests run against fakes and stayed green while an advisory lock was
silently missing from a commit. Anything whose behaviour lives in SQL — locks, transactions, unique
indexes, races — is covered by the integration tests instead.

`cd frontend && npm run shot` drives the running app through Playwright, captures the sign-in page,
the dashboard in both themes and a narrow viewport, and exits non-zero on any console or network
error.
