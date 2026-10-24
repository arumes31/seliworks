# SeliWorks

Self-hosted farm work recording: Go, PostgreSQL and a responsive React/TypeScript PWA. Start work, close the phone, return later, stop, then describe the work. PostgreSQL timestamps determine time; browser activity and application restarts do not stop a session.

One deployment represents one billing organization, with any number of workers and customer farms. Defaults are Europe/Vienna, EUR, German with English selection, actual duration and no automatic breaks. Administrators configure separate vacation/public-holiday surcharges as a percentage or extra currency per hour. Customer charges are not payroll wages.

Administrators can delete stopped work from its details dialog after confirming the entry and entering a reason. Deleted work is excluded from operational lists, reports and totals; original evidence and the deletion audit remain in exports. Active invoice allocations must first be cancelled or reversed through the invoice workflow.

Passkeys are available from Profile, with a setup reminder for accounts without one and a dedicated login button. Each account can name and manage up to 20 passkeys. Registration and removal require the current password and configured MFA; passkey login requires device user verification. Removing a passkey signs out other sessions. Operator password reset and invitation-based account recovery revoke existing passkeys. The relying-party domain and accepted origin come only from `APP_URL`: use a stable HTTPS hostname, or `http://localhost:<port>` for local passkey testing. Changing the hostname requires registering new passkeys. Password login remains available.

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

Configure the existing HTTPS proxy to forward your hostname to `seliworks:16540` on the selected external network (or your configured `PROXY_ALIAS`). Forward the original Host and X-Forwarded-Proto headers. Set TRUSTED_PROXY_CIDRS to the exact proxy addresses; keep PostgreSQL private and do not publish its port in production. Log in with `BOOTSTRAP_EMAIL` and the generated password read privately from `.secrets/bootstrap_password`, then change the password in Profile. Bootstrap refuses to create a second initial administrator.

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

Open [localhost:16540](http://localhost:16540). The override deliberately permits insecure cookies only for local development and binds HTTP/database ports to loopback. No external proxy network is needed. For optional disposable demo records, **skip bootstrap** on a fresh development database and run the following before starting the app. Demo requires no existing users; use a separate project with fresh volumes if you already bootstrapped:

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

This public repository contains application source, database migrations, frontend assets, dependency locks, deployment/backup scripts and GitHub automation. Full test suites, internal documentation, plans and generated reports/screenshots remain local and are intentionally excluded by `.gitignore`. Local credentials, backups, dependencies and build output are also excluded.

## GitHub automation

- **CI** checks formatting, module integrity, Go vet/build, TypeScript, frontend formatting/build, Compose configuration, the non-root image and deployment/workflow lint. It does not claim to run unpublished unit/integration/browser test files.
- **Security** scans Go/frontend dependencies, Git history for credentials, and the built container. High/critical container findings block publication. **CodeQL** analyzes Go, TypeScript and Actions; **Dependency review** checks pull requests. Dependabot updates Go, npm, Docker and Actions weekly.
- **Publish container** is available through Actions → Run workflow on `main`. CI, security and CodeQL must succeed before publishing `ghcr.io/arumes31/seliworks` for AMD64/ARM64, with commit tags, an SBOM and provenance attestations.
- Pushing a version tag such as `v1.0.0` runs the same publication gates and creates a GitHub Release with generated notes and the exact image digest. Stable version tags also update `latest`; prereleases do not. No tag or release is created automatically.
- **Nightly recovery QA** creates disposable runner-only Compose projects and fresh credentials, verifies repeated migrations, records real server work, restarts app/PostgreSQL, then backs up and restores into fresh database/private-file volumes. It does not replace full application suites or physical-device checks.
- **GHCR cleanup** is manual and defaults to dry-run. It considers only unreferenced, untagged manifests older than 30 days, protects retained multi-architecture/attestation children and preserves tagged versions for rollback.

Actions use verified full commit pins and limited job permissions. No production host or external SMTP credentials are required. The small workflow-lint adapter preserves GitHub's current `$/` self-repository syntax while the pinned Actionlint release lacks support (upstream issue #711); it normalizes only equivalent local-workflow references for linting and does not disable rules.

Set organization identity, billing rates, special-day policies and tax treatment before invoicing. Percentage and extra-currency-per-hour vacation/public-holiday policies are configured separately. German is the default interface language; English is selectable.

Production rollout and physical Android/iPhone installation checks still require verification. Keep database/private-document backups and required encryption keys protected outside the deployment host and verify recovery before using real business data.

## Request diagnostics

Unexpected request failures are JSON logs with the server-generated `request_id`, HTTP `method`, registered `route` pattern, error class (`type`) and Go `error_type`. Route parameters, query strings, request bodies, credentials and raw database error messages are excluded. Use the request ID returned by the API to correlate a report with its server event.

`context_canceled` is logged at debug level only when the request context was also canceled by the caller. Internal cancellations and other failures remain errors, even after the caller disconnects. `deadline_exceeded` is a warning; the API still returns the same `503 UNAVAILABLE` response and requires reconciliation before retrying a mutation. Set `LOG_LEVEL=debug` temporarily when investigating canceled requests, then return to the normal `info` level.

New farms default to all workers, including accounts created later. Administrators can instead select individual workers when creating or editing a farm. Existing farms retain their explicit assignments during migration. All-worker assignments are inherited and cannot be removed from an individual worker; change the farm to selected workers to restrict access.
