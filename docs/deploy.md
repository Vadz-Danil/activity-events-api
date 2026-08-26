# Deployment

The stack is split across three free-tier providers:

| Piece                      | Provider                  | Why                                                    |
|----------------------------|---------------------------|--------------------------------------------------------|
| `api`, `worker` containers | Northflank (Sandbox plan) | two always-on services, no sleep, no card required     |
| PostgreSQL                 | Neon                      | 0.5 GB, scales to zero after 5 minutes of inactivity   |
| React SPA                  | Cloudflare Pages          | static hosting, 500 builds per month                   |
| Prometheus, Grafana        | local `docker compose`    | monitoring is demonstrated locally and is not deployed |

The image is built once by GitHub Actions, pushed to GHCR and deployed to both Northflank services by digest-free tag
`ghcr.io/<owner>/<repo>:<commit sha>`.

## Pipeline

`.github/workflows/ci.yml` runs on every push and pull request to `main`:

```
test ────────┐
             ├── build (image → GHCR) ── migrate (goose up) ── deploy-api
frontend ────┤                                              └─ deploy-worker
             └── deploy-frontend (Cloudflare Pages)
```

* `test` — `go vet`, unit tests with `-race`, then migrations and the repository integration tests against a PostgreSQL
  service container.
* `frontend` — `tsc -b && vite build`, the artifact is reused by `deploy-frontend`.
* `build` — only on push, pushes `:<sha>` and `:latest` to GHCR.
* `migrate` — applies migrations to Neon before the new code starts. Migrations are a deploy step, not something the
  application does on boot.
* `deploy-*` — point the Northflank services at the freshly built tag and upload the SPA to Pages.

Deploy jobs are gated on repository variables: while `NORTHFLANK_PROJECT_ID` or
`CLOUDFLARE_PAGES_PROJECT` are unset, those jobs are skipped and the pipeline still passes. Set the variables once the
providers are ready.

## Secrets and variables

Add these in **Settings → Secrets and variables → Actions**.

Secrets (encrypted, masked in logs):

| Name                    | Value                                                      |
|-------------------------|------------------------------------------------------------|
| `DATABASE_URL`          | Neon connection string, `?sslmode=require`                 |
| `NORTHFLANK_API_KEY`    | Northflank API key with the *Update deployment* permission |
| `CLOUDFLARE_API_TOKEN`  | token with *Account → Cloudflare Pages → Edit*             |
| `CLOUDFLARE_ACCOUNT_ID` | Cloudflare account id                                      |

Variables (plain text, safe to read in logs):

| Name                           | Value                                              |
|--------------------------------|----------------------------------------------------|
| `NORTHFLANK_PROJECT_ID`        | Northflank project id                              |
| `NORTHFLANK_API_SERVICE_ID`    | id of the `api` service                            |
| `NORTHFLANK_WORKER_SERVICE_ID` | id of the `worker` service                         |
| `CLOUDFLARE_PAGES_PROJECT`     | Pages project name                                 |
| `VITE_API_URL`                 | public URL of the `api` service, no trailing slash |

`GITHUB_TOKEN` is provisioned by Actions itself and needs no setup.

Secrets never reach the built image: the only build argument is `VERSION`, and runtime configuration is injected by
Northflank as environment variables. Pull requests from forks do not receive secrets, so a fork cannot exfiltrate them.

## One-time provider setup

### 1. Neon

1. Create a project (no card required), pick a region close to Northflank.
2. Copy the **pooled** connection string and add `?sslmode=require`.
3. Store it as the `DATABASE_URL` secret.

### 2. GHCR

The first push to `main` publishes `ghcr.io/<owner>/activity-events-api`. Open **Profile → Packages →
activity-events-api → Package settings** and switch the visibility to **public** — Northflank then pulls it without
registry credentials. To keep the package private instead, add a Northflank registry credential backed by a GitHub token
with `read:packages` and pass its id as
`credentials-id` to the deploy action.

### 3. Northflank

Create a project, then two services of type *Deployment* with **External image**
as the source, both pointing at `ghcr.io/<owner>/activity-events-api:latest`.

`api` — port 8080, HTTP, public:

```
APP_ENV=production
APP_MODE=api
HTTP_PORT=8080
DATABASE_URL=<neon pooled url>
DB_MAX_CONNS=5
DB_MIN_CONNS=0
LOG_LEVEL=info
LOG_FORMAT=json
JWT_SECRET=<32+ random characters>
JWT_ISSUER=activity-events-api
FRONTEND_URL=https://<pages-project>.pages.dev
CORS_ALLOWED_ORIGINS=https://<pages-project>.pages.dev
MAX_BODY_BYTES=65536
GOOGLE_CLIENT_ID=<optional>
GOOGLE_CLIENT_SECRET=<optional>
GOOGLE_REDIRECT_URL=https://<api-host>/api/v1/auth/google/callback
```

`worker` — no public port:

```
APP_ENV=production
APP_MODE=worker
HTTP_PORT=8080
DATABASE_URL=<neon pooled url>
DB_MAX_CONNS=2
DB_MIN_CONNS=0
LOG_LEVEL=info
LOG_FORMAT=json
```

`DB_MIN_CONNS=0` matters: a pool that keeps idle connections open prevents Neon from suspending and burns the monthly
compute allowance.

Health checks: `/healthz` for liveness, `/readyz` for readiness.

Finally create an API key with the *Update deployment* permission and copy the project and service ids into the
repository variables.

### 4. Cloudflare Pages

1. Create a Pages project with **Direct Upload**, production branch `main`.
2. Create an API token with *Account → Cloudflare Pages → Edit*.
3. Store the token and the account id as secrets, the project name as a variable.

`frontend/public/_redirects` maps every path to `index.html`, so the client-side
`/auth/callback` route survives a full page load after the Google redirect.

The SPA reads the API address from `VITE_API_URL` at build time, so changing the API host requires a new frontend
build — a push to `main` is enough.

Set the providers up in this order, each step needs a URL from the previous one:
Neon → Pages project (gives `https://<project>.pages.dev` for `FRONTEND_URL` and
`CORS_ALLOWED_ORIGINS`) → Northflank services (give the API host for
`VITE_API_URL` and `GOOGLE_REDIRECT_URL`) → repository secrets and variables.

## Local monitoring

Prometheus and Grafana stay in `docker-compose.yaml` and are started with
`make infra`. `deploy/prometheus/prometheus.yml` also carries a job for the deployed API, so the local Grafana can graph
production metrics over HTTPS.
