<p align="center">
  <img src="web/public/icon.svg" width="88" alt="SeliWorks">
</p>

# SeliWorks

**Self-hosted time tracking, PDF timesheets, and customer billing for agricultural teams.**

[![CI](https://github.com/arumes31/seliworks/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/arumes31/seliworks/actions/workflows/ci.yml)
[![Security](https://github.com/arumes31/seliworks/actions/workflows/security.yml/badge.svg?branch=main)](https://github.com/arumes31/seliworks/actions/workflows/security.yml)
[![CodeQL](https://github.com/arumes31/seliworks/actions/workflows/codeql.yml/badge.svg?branch=main)](https://github.com/arumes31/seliworks/actions/workflows/codeql.yml)

Start work, put your phone away, and document the activity when you return. SeliWorks records time on the server: closing the browser or restarting the application does not end a running session. The responsive, installable PWA offers **German by default**, an English language option, and light and dark themes.

Each installation represents one billing organization with multiple workers, administrators, and farms or customers. Defaults are **Europe/Vienna**, **EUR**, and actual elapsed time without automatic break deductions. Billing covers customer services; it is not payroll.

[Features](#features) · [Installation](#installation) · [Operations](#operations) · [Development](#development) · [Quality assurance](#quality-assurance)

## Features

| Area                | Capabilities                                                                                                                                            |
| ------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Time tracking       | Start and stop work, pause for a specified duration or until resumed, record activities and descriptions, and apply additional time deductions          |
| Farms and customers | Assign individual workers and administrators, or automatically include all current and future users                                                     |
| Administration      | Edit complete entries, add breaks, correct times, and transfer work to another farm                                                                     |
| Review              | Approve or reject entries; delete stopped work through a confirmation modal with a required reason                                                      |
| Timesheets          | Download personal PDF timesheets; administrators can export one, several, or all workers with separate pages per person                                 |
| Billing             | Hourly rates, separate vacation-day and public-holiday surcharges in percent or additional currency per hour, invoice PDFs, and optional email delivery |
| Authentication      | Password login, optional authenticator-based MFA, and multiple named passkeys per account                                                               |

### Roles and farm assignments

Administrator and worker permissions are independent and can be combined. Administrators can assign farms to other administrators, including accounts without worker permission. Assignment does not grant permission to record work; that still requires the worker role.

New farms default to **All users – including future users**. Choose **Selected users** to restrict assignments. Inherited assignments are managed on the farm and cannot be removed from an individual user. Existing explicit assignments survive migrations. Administrators retain organization-wide management access.

### Corrections and transfers

Open **Work entries → Details → Edit complete entry**. The **Farm / customer** selector appears at the top of the form; selecting another farm transfers the entry. Historical work can also be transferred after its existing owner loses worker permission.

The original recording and original farm remain documented. Changes are audited and require fresh approval. Running work must be stopped before editing or deletion. Active invoice allocations block changes and must first be released through the invoice workflow. Deleted work disappears from operational lists and totals, while original evidence and the deletion reason remain in the audit export.

### PDF timesheets

Work entries support multiple selected activities and an optional **Additional activities** text field (up to 1,000 characters). The selected **Primary activity** determines the hourly rate and activity report grouping; work time is counted once. Additional selections and text are retained in timesheets, invoice detail snapshots, and business exports.

Summary-only invoice PDFs retain the primary activity/rate grouping. Enable the detailed appendix, or use the detailed invoice layout, to include the additional activity evidence in the invoice PDF.

- **Workers:** download their own timesheet from History.
- **Administrators:** open the employee timesheet PDF export in Work entries and select one, several, or all people.
- Both start and end dates are included, using the organization's timezone.
- PDFs include completed work, gross duration, breaks, additional deductions, and net time. Entries crossing the selected period are clipped to its boundaries.
- Each person starts on a new page. Long descriptions continue across pages, with separate totals for each person.

Each export supports up to one year, 100 people, 2,000 entries, and 1 MB of entry text. Split larger exports by date or person. Timesheets are separate from customer invoices.

### Passkeys and login

In **Profile**, each account can register, name, and remove up to 20 passkeys. Accounts without a passkey receive a setup reminder; **Remind me later** postpones it by three to seven days. Password login remains available. The authenticator field appears only when required for login.

Registration and removal require the current password and configured MFA. Passkey login requires device user verification. Removing a passkey signs out other sessions; operator password resets and invitation-based account recovery revoke existing passkeys.

Passkeys are tied to the domain configured in `APP_URL`. Use a stable HTTPS hostname. Changing the domain requires registering new passkeys; `http://localhost:16540` is supported for local testing.

## Installation

### Requirements

- Linux host with Docker and Docker Compose v2
- Git and OpenSSL; backup scripts additionally require Python 3 and `flock`
- A domain, HTTPS reverse proxy, and an existing external Docker network

Shell examples use POSIX syntax. Windows users can run them through WSL or Git Bash. Keep credentials and backups in a protected directory on the server.

### Docker Compose with HTTPS

```sh
git clone https://github.com/arumes31/seliworks.git
cd seliworks
cp .env.example .env
```

Update `.env` before starting:

| Variable                            | Purpose                                                                   |
| ----------------------------------- | ------------------------------------------------------------------------- |
| `APP_URL`                           | Public HTTPS origin without an additional path                            |
| `PROXY_NETWORK`                     | Existing reverse-proxy network, such as `npm_default`                     |
| `PROXY_ALIAS`                       | Internal application hostname; defaults to `seliworks`                    |
| `TRUSTED_PROXY_CIDRS`               | Actual proxy addresses or narrowly scoped networks; never global networks |
| `BOOTSTRAP_EMAIL`, `BOOTSTRAP_NAME` | Identity of the initial administrator                                     |
| `BACKUP_DIR`                        | Local backup directory; defaults to `./backups`                           |
| `SMTP_*`                            | Optional invoice delivery; an empty `SMTP_HOST` disables it               |

See [.env.example](.env.example) for the complete settings. Passwords and keys are supplied as files under `.secrets/`.

```sh
sh scripts/init-secrets.sh
sudo chown 10001:10001 backups
chmod 700 backups
docker compose build
docker compose up -d --wait postgres
docker compose --profile tools run --rm migrate
docker compose --profile tools run --rm bootstrap
docker compose up -d --wait app
docker compose ps
```

Log in with `BOOTSTRAP_EMAIL` and the password read privately from `.secrets/bootstrap_password`, then change the password in Profile. Bootstrap creates only the initial administrator. Migrations and initial user creation run explicitly before the application starts, never implicitly when the web server starts.

### Nginx Proxy Manager and networking

Create an NPM Proxy Host for the configured domain:

| Setting          | Value                                                |
| ---------------- | ---------------------------------------------------- |
| Forward Scheme   | `http`                                               |
| Forward Hostname | `seliworks`, or the configured `PROXY_ALIAS`         |
| Forward Port     | `16540`                                              |
| Docker network   | The same external network as `PROXY_NETWORK`         |
| TLS              | A certificate for the domain, with HTTPS redirection |

The proxy must forward the original `Host` and `X-Forwarded-Proto` headers. Production Compose does not publish an application port on the host. PostgreSQL is attached only to the internal database network. See [compose.yaml](compose.yaml) for the full configuration.

### Persistent storage

Compose uses the named volumes `postgres-data` and `app-data` by default. Recreating containers preserves their data.

For a **new installation** with fixed host paths, such as `/container/seliworks/`, create the directories first:

```sh
sudo install -d -m 700 -o 70 -g 70 /container/seliworks/postgres
sudo install -d -m 700 -o 10001 -g 10001 /container/seliworks/data
```

The pinned Alpine PostgreSQL image uses UID/GID `70:70`; the application uses `10001:10001`. Recheck these IDs when changing image families.

Add a `compose.override.yaml` in the installation directory. Compose automatically includes it in the commands above:

```yaml
volumes:
  postgres-data:
    driver: local
    driver_opts:
      type: none
      o: bind
      device: /container/seliworks/postgres
  app-data:
    driver: local
    driver_opts:
      type: none
      o: bind
      device: /container/seliworks/data
```

This does not move existing volumes into those directories. Switching storage paths later requires a planned data migration. Also protect `.env`, `.secrets/`, Compose configuration, and backups.

### Local development environment

Include both Compose files on **every** local command:

```sh
cp .env.example .env
# Set BOOTSTRAP_EMAIL and BOOTSTRAP_NAME in .env.
sh scripts/init-secrets.sh
docker compose -f compose.yaml -f compose.dev.yaml build
docker compose -f compose.yaml -f compose.dev.yaml up -d --wait postgres
docker compose -f compose.yaml -f compose.dev.yaml --profile tools run --rm migrate
docker compose -f compose.yaml -f compose.dev.yaml --profile tools run --rm bootstrap
docker compose -f compose.yaml -f compose.dev.yaml up -d --wait app
```

Open [localhost:16540](http://localhost:16540). The [development override](compose.dev.yaml) binds application and database ports to loopback and does not need an external proxy network. Its HTTP configuration is intended only for local development.

<details>
<summary>Optional demo data in a fresh development database</summary>

Skip bootstrap and run the following after migration. The database must not contain any users:

```sh
openssl rand -hex 24 > .secrets/demo_password
docker compose -f compose.yaml -f compose.dev.yaml run --rm \
  -v "$PWD/.secrets/demo_password:/run/secrets/demo_password:ro" \
  -e DEMO_PASSWORD_FILE=/run/secrets/demo_password app demo
```

Demo creates `demo@seliworks.local` and `worker@seliworks.local` with the generated password. It is refused in production. Use a separate Compose project with fresh volumes for further disposable trials.

</details>

## Operations

### Updates

Create a matching backup before a routine update. Then update the source, build, migrate, and replace the application:

```sh
git pull --ff-only
sudo docker compose build
sudo docker compose --profile tools run --rm migrate
sudo docker compose up -d --no-deps --wait app
sudo docker compose ps
```

The PWA applies application updates automatically when there are no unsaved forms or pending writes. A notice remains visible when unsaved input needs attention first.

The Docker build runs the production frontend build: JavaScript bundles and the service worker are minified with Vite's Oxc minifier, with source maps disabled. GHCR images use the same Dockerfile; readable source files remain in the repository.

Prebuilt AMD64 and ARM64 images are published to [ghcr.io/arumes31/seliworks](https://github.com/arumes31/seliworks/pkgs/container/seliworks). Use a verified digest for reproducible installations, and configure **the application, migration, and bootstrap services to use the same image**. Builds from `main` receive commit tags; `latest` is updated only by stable version releases.

### Backup and recovery

[scripts/backup.sh](scripts/backup.sh) creates a matched backup of the database, private files, `.env`, and the files under `.secrets/`. It briefly stops the application for consistency, then restarts it if it was running. Recorded work clocks remain in the database.

Prepare the configuration permissions before the first backup, then run the backup as root:

```sh
sudo chown root:root .env .secrets
sudo chmod 640 .env
sudo chmod 700 .secrets
sudo find .secrets -maxdepth 1 -type f -exec chown root:root {} +
sudo find .secrets -maxdepth 1 -type f -exec chmod 644 {} +
sudo sh scripts/backup.sh
```

The private `0700` parent directory protects secret files on the host; the files remain readable by the non-root container user through Docker's secret mounts. After this preparation, run Compose commands with `sudo` and edit `.env` with `sudoedit`. Installation checks require root-owned configuration files with these permissions. Local backups are protected by file permissions, **not encryption**. Encrypt the complete backup, including key material, before copying it off-host.

Restore into a separate Compose project with an empty database and empty application volume. [installation-backup.py](scripts/installation-backup.py) verifies or reconstructs the matching configuration; [restore.sh](scripts/restore.sh) restores the database and files. Adjust proxy aliases, host paths, and secrets for the separate recovery project. Verify users, running timers, rates, invoices, and PDF downloads before admitting traffic.

### Diagnostics

```sh
sudo docker compose ps
sudo docker compose logs --tail=100 app
curl --fail https://work.example.com/health/ready
```

Replace the health-check hostname with your domain. Request failures include a server-generated `request_id`, HTTP method, route pattern, and error class. Parameters, request contents, credentials, and raw database errors are excluded. Use the request ID to correlate API failures with server logs.

Temporarily set `LOG_LEVEL=debug` to inspect client-cancelled requests. Timeouts are logged as warnings. After an interrupted write, refresh the actual state before retrying the operation.

Configure organization details, tax settings, hourly rates, and vacation/public-holiday policies before issuing invoices.

## Development

The backend is written in Go, with a React and TypeScript frontend. PostgreSQL provides durable records and timestamps. The production container serves built frontend assets directly from Go; Node is not required at runtime.

Authoritative versions are pinned in [go.mod](go.mod), [Dockerfile](Dockerfile), [compose.yaml](compose.yaml), and the [CI workflow](.github/workflows/ci.yml). Frontend dependencies are locked in [package-lock.json](web/package-lock.json).

```sh
cd web
npm ci
npm run build
cd ..
go vet ./...
go build -trimpath -o seliworks ./cmd/seliworks
```

For a direct runtime, configure `DATABASE_URL` or `DATABASE_URL_FILE`, `APP_URL`, `APP_ENV`, `DATA_DIR`, and `STATIC_DIR`. Run `./seliworks migrate`, `./seliworks bootstrap`, then `./seliworks serve`. Bootstrap also requires the administrator identity and `BOOTSTRAP_PASSWORD_FILE`; TOTP uses `TOTP_KEY_FILE`.

Production is the default and requires an HTTPS `APP_URL` before startup. Session and passkey cookies use `Secure`, `HttpOnly`, and strict SameSite protection. HTTP cookies are permitted only with `APP_ENV=development` and a loopback origin (`localhost`, `127.0.0.1`, or `::1`). Keep `.env`, `.secrets/`, and local `.tools/` files out of Git and container images; CI checks the tracked paths and scans Git history for credentials.

| Directory                  | Contents                                                        |
| -------------------------- | --------------------------------------------------------------- |
| `cmd/seliworks/`           | Entry point and operational commands                            |
| `internal/app/`            | API, authentication, time tracking, billing, and PDF generation |
| `internal/app/migrations/` | Versioned database migrations                                   |
| `web/src/`                 | React interface and translations                                |
| `web/public/`              | PWA manifest, service worker, and application icons             |
| `scripts/`                 | Secrets, database initialization, backup, and recovery          |
| `.github/workflows/`       | Checks, publishing, and recovery verification                   |

## Quality assurance

| Workflow                       | Coverage                                                                                                                                                                     |
| ------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| CI                             | Go formatting/build and `go vet`, TypeScript and frontend build, browser regressions, PostgreSQL regressions with race detection, PDF pagination, Compose, and script checks |
| Security                       | Go/npm dependencies, secrets in Git history, and container vulnerabilities; high and critical image findings block publication                                               |
| CodeQL                         | Go, JavaScript/TypeScript, Python, and GitHub Actions                                                                                                                        |
| Dependency review / Dependabot | Dependency changes in PRs and scheduled updates for Go, npm, Docker, and Actions                                                                                             |
| Publish container / Release    | Gated publication, multiarch images, SBOMs, and provenance attestations; version tags additionally create GitHub Releases                                                    |
| Nightly recovery QA            | Isolated Compose installation, migrations, real server time recording, restarts, and database/private-file restoration                                                       |
| GHCR cleanup                   | Manual cleanup of old, unreferenced images; defaults to a dry run                                                                                                            |

Published regressions are included under `internal/app/*_test.go` and `web/test/` through explicit exceptions in [.gitignore](.gitignore). Additional local tests, internal documentation, screenshots, and generated reports are not published. The workflows define the exact hosted verification scope.

Mobile layouts are tested using virtual phone viewports. This does not establish acceptance on physical Android or iPhone devices.
