# SeliWorks

Self-hosted farm work recording: Go, PostgreSQL and a responsive React/TypeScript PWA. Start work, close the phone, return later, stop, then describe the work. PostgreSQL timestamps determine time; browser activity and application restarts do not stop a session.

One deployment represents one billing organization, with any number of workers and customer farms. Defaults are Europe/Vienna, EUR, German with English selection, actual duration and no automatic breaks. Administrators configure separate vacation/public-holiday surcharges as a percentage or extra currency per hour. Customer charges are not payroll wages.

## Run with Docker Compose

Requirements: a Linux Docker host with Compose v2, an existing HTTPS reverse proxy and its external Docker network, DNS, and OpenSSL. PowerShell users can run the POSIX operational scripts through WSL or Git Bash. Keep secrets and backups outside synchronized/shared folders on the production host.

```sh
cp .env.example .env
# Edit APP_URL, PROXY_NETWORK, TRUSTED_PROXY_CIDRS and administrator identity.
sh scripts/init-secrets.sh
sudo chown 10001:10001 backups
chmod 700 backups
docker compose build
docker compose up -d postgres
docker compose --profile tools run --rm migrate
docker compose --profile tools run --rm bootstrap
docker compose up -d app
docker compose ps
```

Configure the existing HTTPS proxy to forward your hostname to `seliworks:8080` on the selected external network (or your configured `PROXY_ALIAS`). Forward the original Host and X-Forwarded-Proto headers. Set TRUSTED_PROXY_CIDRS to the exact proxy addresses; keep PostgreSQL private and do not publish its port in production. Log in with `BOOTSTRAP_EMAIL` and the generated password read privately from `.secrets/bootstrap_password`, then change the password in Profile. Bootstrap refuses to create a second initial administrator.

For local development, use the override on **every** Compose invocation:

```sh
cp .env.example .env
sh scripts/init-secrets.sh
docker compose -f compose.yaml -f compose.dev.yaml build
docker compose -f compose.yaml -f compose.dev.yaml up -d postgres
docker compose -f compose.yaml -f compose.dev.yaml --profile tools run --rm migrate
docker compose -f compose.yaml -f compose.dev.yaml --profile tools run --rm bootstrap
docker compose -f compose.yaml -f compose.dev.yaml up -d app
```

Open [localhost:8080](http://localhost:8080). The override deliberately permits insecure cookies only for local development and binds HTTP/database ports to loopback. No external proxy network is needed. For optional disposable demo records, **skip bootstrap** on a fresh development database and run the following before starting the app. Demo requires no existing users; use a separate project with fresh volumes if you already bootstrapped:

```sh
# Generate the development password locally; do not reuse any production password.
openssl rand -hex 24 > .secrets/demo_password
docker compose -f compose.yaml -f compose.dev.yaml run --rm \
  -v "$PWD/.secrets/demo_password:/run/secrets/demo_password:ro" \
  -e DEMO_PASSWORD_FILE=/run/secrets/demo_password app demo
```

Demo creates `demo@seliworks.local` and `worker@seliworks.local` with that supplied password. It is refused in production. Do not run demo on a real organization database.

## Build from source

Go 1.27.2, Node 24.22.0 and PostgreSQL 18.6 are the pinned baseline. Node builds frontend assets; production serves them directly from Go.

```sh
cd web
npm ci
npm run build
cd ..
go vet ./...
go build -trimpath -o seliworks ./cmd/seliworks
```

For direct runtime use, configure `DATABASE_URL` or `DATABASE_URL_FILE`, `APP_URL`, `APP_ENV`, `DATA_DIR` and `STATIC_DIR`. Run `./seliworks migrate`, `./seliworks bootstrap`, then `./seliworks serve`. Bootstrap requires `BOOTSTRAP_EMAIL`, `BOOTSTRAP_NAME` and `BOOTSTRAP_PASSWORD_FILE` and refuses to create a second initial administrator.

## Repository contents

This public repository contains application source, database migrations, frontend assets, dependency locks and deployment/backup scripts. Tests, internal documentation, plans, generated reports/screenshots and test-only CI remain local and are intentionally excluded by `.gitignore`. Local credentials, backups, dependencies and build output are also excluded.

Set organization identity, billing rates, special-day policies and tax treatment before invoicing. Percentage and extra-currency-per-hour vacation/public-holiday policies are configured separately. German is the default interface language; English is selectable.

Production rollout and physical Android/iPhone installation checks still require verification. Keep database/private-document backups and required encryption keys protected outside the deployment host and verify recovery before using real business data.
