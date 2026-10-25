<p align="center">
  <img src="web/public/icon.svg" width="88" alt="SeliWorks">
</p>

# SeliWorks

**Zeiterfassung, Zeitnachweise und Kundenabrechnung für landwirtschaftliche Betriebe.**

[![CI](https://github.com/arumes31/seliworks/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/arumes31/seliworks/actions/workflows/ci.yml)
[![Security](https://github.com/arumes31/seliworks/actions/workflows/security.yml/badge.svg?branch=main)](https://github.com/arumes31/seliworks/actions/workflows/security.yml)
[![CodeQL](https://github.com/arumes31/seliworks/actions/workflows/codeql.yml/badge.svg?branch=main)](https://github.com/arumes31/seliworks/actions/workflows/codeql.yml)

Arbeit starten, das Handy weglegen und später die Tätigkeit dokumentieren. SeliWorks erfasst Zeiten auf dem Server: Ein geschlossener Browser oder ein Neustart beendet keine laufende Arbeit. Die Oberfläche ist eine responsive, installierbare PWA mit **Deutsch als Standardsprache**, englischer Sprachwahl sowie hellem und dunklem Design.

Eine Installation verwaltet eine Abrechnungsorganisation mit mehreren Mitarbeitenden, Administratoren und Betrieben beziehungsweise Kunden. Standardwerte sind **Europe/Vienna**, **EUR** und die tatsächliche Arbeitsdauer ohne automatisch abgezogene Pausen. Die Abrechnung betrifft Kundenleistungen; sie ist keine Lohnabrechnung.

[Funktionsumfang](#funktionsumfang) · [Installation](#installation) · [Betrieb](#betrieb) · [Entwicklung](#entwicklung) · [Qualitätssicherung](#qualitätssicherung)

## Funktionsumfang

| Bereich       | Möglichkeiten                                                                                                                                               |
| ------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Zeiterfassung | Starten und Stoppen, Pause mit Zeitangabe oder bis zur Fortsetzung, Tätigkeiten, Beschreibungen und zusätzliche Zeitabzüge                                  |
| Betriebe      | Einzelzuweisung an Mitarbeitende und Administratoren oder automatisch an alle bestehenden und zukünftigen Benutzer                                          |
| Verwaltung    | Einträge vollständig bearbeiten, Pausen ergänzen, Zeiten korrigieren und Arbeit auf einen anderen Betrieb umbuchen                                          |
| Prüfung       | Genehmigen, zurückweisen und gestoppte Einträge nach Bestätigung im Modal mit Begründung löschen                                                            |
| Zeitnachweise | Eigene Einträge als PDF; Admin-Export für einzelne, ausgewählte oder alle Mitarbeitenden mit getrennten Seiten je Person                                    |
| Abrechnung    | Stundensätze, getrennte Urlaubs- und Feiertagszuschläge in Prozent oder als zusätzlicher Eurobetrag je Stunde, Rechnungs-PDFs und optionaler E-Mail-Versand |
| Anmeldung     | Passwort, optionale Authenticator-Absicherung und mehrere benannte Passkeys pro Benutzer                                                                    |

### Rollen und Betriebszuweisung

Administrator- und Mitarbeitendenrolle sind unabhängig und können kombiniert werden. Administratoren können auch anderen Administratoren Betriebe zuweisen, selbst wenn diese keine Mitarbeitendenrolle haben. Eine Betriebszuweisung schaltet die Zeiterfassung nicht zusätzlich frei: Dafür ist die Mitarbeitendenrolle erforderlich.

Neue Betriebe verwenden standardmäßig **„Alle Benutzer – auch zukünftige“**. Für eine eingeschränkte Zuweisung lässt sich **„Einzelne Benutzer“** auswählen. Geerbte Zuweisungen werden am Betrieb verwaltet; sie können nicht bei einem einzelnen Benutzer entfernt werden. Bestehende explizite Zuweisungen bleiben bei Migrationen erhalten. Administratoren behalten ihre organisationsweiten Verwaltungsrechte.

### Arbeit korrigieren und umbuchen

Unter **Arbeitseinträge → Details anzeigen → Eintrag vollständig bearbeiten** steht die Auswahl **Betrieb / Kunde** direkt oben. Ein anderer Betrieb bucht den Eintrag um. Das funktioniert auch bei historischen Einträgen, deren Besitzer die Mitarbeitendenrolle inzwischen verloren hat.

Die ursprüngliche Aufzeichnung und der ursprüngliche Betrieb bleiben dokumentiert. Änderungen werden protokolliert und erfordern eine erneute Genehmigung. Laufende Arbeit muss vor der Bearbeitung oder Löschung gestoppt werden. Aktive Rechnungszuordnungen sperren Änderungen; diese müssen zuerst über den Rechnungsablauf aufgehoben werden. Gelöschte Arbeit verschwindet aus operativen Listen und Summen, bleibt aber mit Originaldaten und Löschbegründung im Prüfexport erhalten.

### PDF-Zeitnachweise

- **Mitarbeitende:** im Verlauf den eigenen Zeitnachweis herunterladen.
- **Administratoren:** unter Arbeitseinträge **„Zeitnachweise Mitarbeitende (PDF)“** öffnen und eine, mehrere oder alle Personen auswählen.
- Anfangs- und Enddatum sind eingeschlossen; maßgeblich ist die Zeitzone der Organisation.
- Die PDFs enthalten beendete Arbeit mit Bruttozeit, Pausen, zusätzlichen Abzügen und Nettozeit. Einträge an den Zeitraumgrenzen werden entsprechend zugeschnitten.
- Jede Person beginnt auf einer neuen Seite. Lange Beschreibungen werden über Seiten fortgesetzt; Summen bleiben je Person getrennt.

Ein Export umfasst höchstens ein Jahr, 100 Personen, 2.000 Einträge und insgesamt 1 MB Eintragstext. Bei größeren Datenmengen den Zeitraum oder die Personenauswahl aufteilen. Ein Zeitnachweis ist unabhängig von der Kundenrechnung.

### Passkeys und Anmeldung

Unter **Profil** können pro Konto bis zu 20 Passkeys eingerichtet, benannt und entfernt werden. Ohne Passkey erscheint eine Einrichtungserinnerung; **„Später erinnern“** verschiebt sie um drei bis sieben Tage. Passwortanmeldung bleibt verfügbar. Das Authenticator-Feld erscheint erst, wenn es für die Anmeldung benötigt wird.

Einrichtung und Entfernung erfordern das aktuelle Passwort sowie eine eingerichtete MFA-Bestätigung. Passkey-Anmeldung verlangt die Benutzerbestätigung am Gerät. Eine Entfernung meldet andere Sitzungen ab; Passwortzurücksetzung durch den Betreiber und Kontowiederherstellung per Einladung widerrufen vorhandene Passkeys.

Passkeys sind an die in `APP_URL` konfigurierte Domain gebunden. Verwende eine stabile HTTPS-Adresse. Nach einem Domainwechsel müssen Passkeys neu eingerichtet werden; lokal ist `http://localhost:16540` möglich.

## Installation

### Voraussetzungen

- Linux-Host mit Docker und Docker Compose v2
- Git und OpenSSL; für die Sicherungsskripte zusätzlich Python 3 und `flock`
- Domain mit HTTPS-Reverse-Proxy und vorhandenem externen Docker-Netzwerk

Die Shell-Beispiele sind für POSIX-Shells gedacht; unter Windows lassen sie sich über WSL oder Git Bash ausführen. Zugangsdaten und Sicherungen gehören in ein geschütztes Verzeichnis auf dem Server.

### Docker Compose mit HTTPS

```sh
git clone https://github.com/arumes31/seliworks.git
cd seliworks
cp .env.example .env
```

Vor dem Start die Werte in `.env` anpassen:

| Variable                            | Bedeutung                                                                             |
| ----------------------------------- | ------------------------------------------------------------------------------------- |
| `APP_URL`                           | Öffentliche HTTPS-Adresse ohne zusätzlichen Pfad                                      |
| `PROXY_NETWORK`                     | Bereits vorhandenes Netzwerk des Reverse-Proxys, zum Beispiel `npm_default`           |
| `PROXY_ALIAS`                       | Interner Hostname der App; Standard `seliworks`                                       |
| `TRUSTED_PROXY_CIDRS`               | Tatsächliche Proxy-Adressen beziehungsweise eng begrenzte Netze; keine globalen Netze |
| `BOOTSTRAP_EMAIL`, `BOOTSTRAP_NAME` | Identität des ersten Administrators                                                   |
| `BACKUP_DIR`                        | Verzeichnis für lokale Sicherungen; Standard `./backups`                              |
| `SMTP_*`                            | Optionaler Rechnungsversand; leerer `SMTP_HOST` deaktiviert ihn                       |

Weitere Vorgaben stehen in [.env.example](.env.example). Passwörter und Schlüssel werden als Dateien unter `.secrets/` bereitgestellt.

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

Mit `BOOTSTRAP_EMAIL` und dem privat aus `.secrets/bootstrap_password` gelesenen Passwort anmelden und das Passwort im Profil ändern. `bootstrap` erstellt nur den ersten Administrator. Migrationen und Benutzeranlage erfolgen ausdrücklich vor dem App-Start, nicht automatisch beim Start des Webservers.

### Nginx Proxy Manager und Netzwerk

In NPM einen Proxy Host für die konfigurierte Domain anlegen:

| Einstellung      | Wert                                              |
| ---------------- | ------------------------------------------------- |
| Forward Scheme   | `http`                                            |
| Forward Hostname | `seliworks` beziehungsweise `PROXY_ALIAS`         |
| Forward Port     | `16540`                                           |
| Docker-Netzwerk  | Dasselbe externe Netzwerk wie `PROXY_NETWORK`     |
| TLS              | Zertifikat für die Domain und HTTPS-Weiterleitung |

Der Proxy muss den ursprünglichen `Host` und `X-Forwarded-Proto` weiterreichen. Die Produktionskonfiguration veröffentlicht keinen App-Port auf dem Host. PostgreSQL liegt ausschließlich im internen Datenbanknetzwerk. Die vollständige Konfiguration steht in [compose.yaml](compose.yaml).

### Persistente Daten

Standardmäßig verwendet Compose die benannten Volumes `postgres-data` und `app-data`. Daten bleiben beim Neuerstellen der Container erhalten.

Für eine **neue Installation** mit festen Hostpfaden, etwa unter `/container/seliworks/`, zuerst die Verzeichnisse anlegen:

```sh
sudo install -d -m 700 /container/seliworks/postgres
sudo install -d -m 700 -o 10001 -g 10001 /container/seliworks/data
```

Dann im Installationsverzeichnis eine `compose.override.yaml` ergänzen. Compose berücksichtigt sie bei den oben gezeigten Befehlen automatisch:

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

Bestehende Volumes werden dadurch nicht in die neuen Verzeichnisse verschoben. Ein späterer Wechsel benötigt eine geplante Datenmigration. `.env`, `.secrets/`, Compose-Konfiguration und Datensicherungen ebenfalls geschützt aufbewahren.

### Lokal ausprobieren

Für lokale Entwicklung auf **jedem** Compose-Befehl beide Dateien angeben:

```sh
cp .env.example .env
# BOOTSTRAP_EMAIL und BOOTSTRAP_NAME in .env setzen.
sh scripts/init-secrets.sh
docker compose -f compose.yaml -f compose.dev.yaml build
docker compose -f compose.yaml -f compose.dev.yaml up -d --wait postgres
docker compose -f compose.yaml -f compose.dev.yaml --profile tools run --rm migrate
docker compose -f compose.yaml -f compose.dev.yaml --profile tools run --rm bootstrap
docker compose -f compose.yaml -f compose.dev.yaml up -d --wait app
```

Die App ist unter [localhost:16540](http://localhost:16540) erreichbar. [compose.dev.yaml](compose.dev.yaml) bindet App und Datenbank nur an Loopback und benötigt kein externes Proxy-Netzwerk. Diese HTTP-Konfiguration ist ausschließlich für lokale Entwicklung bestimmt.

<details>
<summary>Optionale Demo-Daten in einer frischen Entwicklungsdatenbank</summary>

Den Bootstrap-Schritt überspringen und nach der Migration folgenden Befehl ausführen. Die Datenbank darf noch keine Benutzer enthalten:

```sh
openssl rand -hex 24 > .secrets/demo_password
docker compose -f compose.yaml -f compose.dev.yaml run --rm \
  -v "$PWD/.secrets/demo_password:/run/secrets/demo_password:ro" \
  -e DEMO_PASSWORD_FILE=/run/secrets/demo_password app demo
```

Demo erstellt `demo@seliworks.local` und `worker@seliworks.local` mit dem erzeugten Passwort. Der Befehl wird im Produktionsmodus abgelehnt. Für weitere Versuche ein separates Compose-Projekt mit frischen Volumes verwenden.

</details>

## Betrieb

### Updates

Vor einem regulären Update eine passende Sicherung erstellen. Anschließend den Quellstand aktualisieren, bauen, migrieren und die App ersetzen:

```sh
git pull --ff-only
docker compose build
docker compose --profile tools run --rm migrate
docker compose up -d --no-deps --wait app
docker compose ps
```

Die PWA übernimmt App-Updates automatisch, sobald dies ohne Verlust offener Formulare und laufender Anfragen möglich ist. Ein Hinweis bleibt sichtbar, wenn zunächst offene Eingaben gesichert werden müssen.

Vorgebaute Images werden unter [ghcr.io/arumes31/seliworks](https://github.com/arumes31/seliworks/pkgs/container/seliworks) für AMD64 und ARM64 veröffentlicht. Für reproduzierbare Installationen einen geprüften Digest verwenden und **App, Migrations- und Bootstrap-Service auf dasselbe Image setzen**. Ein Build von `main` erzeugt Commit-Tags; `latest` wird nur bei stabilen Versions-Releases aktualisiert.

### Sicherung und Wiederherstellung

[scripts/backup.sh](scripts/backup.sh) erstellt eine zusammengehörige Sicherung aus Datenbank, privaten Dateien sowie `.env` und den Dateien unter `.secrets/`. Es stoppt die App kurz für einen konsistenten Stand und startet eine zuvor laufende App anschließend wieder. Laufende Arbeitszeiten bleiben in der Datenbank bestehen.

```sh
sudo sh scripts/backup.sh
```

Die Installationsprüfung verlangt root-eigene Konfigurationsdateien mit passenden Zugriffsrechten. Lokale Sicherungen sind durch Dateirechte geschützt, **nicht verschlüsselt**. Vor einer Kopie auf ein externes Ziel die gesamte Sicherung einschließlich Schlüsselmaterial verschlüsseln.

Eine Wiederherstellung erfolgt in einem separaten Compose-Projekt mit leerer Datenbank und leerem Datenvolume. [installation-backup.py](scripts/installation-backup.py) prüft beziehungsweise rekonstruiert die zugehörige Konfiguration; [restore.sh](scripts/restore.sh) stellt Datenbank und Dateien wieder her. Dabei müssen Proxy-Alias, Hostpfade und Secrets auf das separate Wiederherstellungsprojekt abgestimmt sein. Vor Freigabe Benutzer, laufende Zeiten, Sätze, Rechnungen und PDF-Downloads prüfen.

### Diagnose

```sh
docker compose ps
docker compose logs --tail=100 app
curl --fail https://work.example.com/health/ready
```

Die Health-URL an die eigene Domain anpassen. Anfragefehler enthalten eine serverseitige `request_id`, HTTP-Methode, Routenmuster und Fehlerklasse. Parameter, Anfrageinhalte, Zugangsdaten und rohe Datenbankfehler werden dabei nicht protokolliert. Die Request-ID verbindet API-Fehlermeldung und Serverlog.

`LOG_LEVEL=debug` macht vom Client abgebrochene Anfragen sichtbar. Zeitüberschreitungen erscheinen als Warnung. Nach einer unterbrochenen schreibenden Anfrage zuerst den tatsächlichen Zustand aktualisieren, bevor derselbe Vorgang wiederholt wird.

Vor der ersten Rechnung Organisationsdaten, Steuereinstellungen, Stundensätze und Urlaubs-/Feiertagszuschläge konfigurieren.

## Entwicklung

Das Backend ist in Go geschrieben, die Oberfläche in React und TypeScript. PostgreSQL liefert persistente Daten und Zeitstempel. Im Produktionscontainer liefert Go die gebauten Frontend-Dateien direkt aus; Node ist dort nicht erforderlich.

Die verbindlichen Versionen stehen in [go.mod](go.mod), [Dockerfile](Dockerfile), [compose.yaml](compose.yaml) und den [CI-Workflows](.github/workflows/ci.yml). Frontend-Abhängigkeiten sind in [package-lock.json](web/package-lock.json) festgeschrieben.

```sh
cd web
npm ci
npm run build
cd ..
go vet ./...
go build -trimpath -o seliworks ./cmd/seliworks
```

Für den direkten Start `DATABASE_URL` oder `DATABASE_URL_FILE`, `APP_URL`, `APP_ENV`, `DATA_DIR` und `STATIC_DIR` konfigurieren. Danach `./seliworks migrate`, `./seliworks bootstrap` und `./seliworks serve` ausführen. Bootstrap benötigt zusätzlich die Administratoridentität und `BOOTSTRAP_PASSWORD_FILE`; TOTP verwendet `TOTP_KEY_FILE`.

| Verzeichnis                | Inhalt                                                             |
| -------------------------- | ------------------------------------------------------------------ |
| `cmd/seliworks/`           | Einstiegspunkt und Betriebsbefehle                                 |
| `internal/app/`            | API, Authentifizierung, Zeiterfassung, Abrechnung und PDFs         |
| `internal/app/migrations/` | Versionierte Datenbankmigrationen                                  |
| `web/src/`                 | React-Oberfläche und Übersetzungen                                 |
| `web/public/`              | PWA-Manifest, Service Worker und App-Symbole                       |
| `scripts/`                 | Secrets, Datenbankinitialisierung, Sicherung und Wiederherstellung |
| `.github/workflows/`       | Prüfungen, Veröffentlichung und Wiederherstellungstests            |

## Qualitätssicherung

| Workflow                       | Geprüfter Umfang                                                                                                                                                                   |
| ------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| CI                             | Go-Formatierung und Build, `go vet`, TypeScript, Frontend-Build, Browserregressionen, PostgreSQL-Regressionen mit Race Detection, PDF-Seitenumbrüche, Compose- und Skriptprüfungen |
| Security                       | Go-/npm-Abhängigkeiten, Secrets in der Git-Historie und Container-Schwachstellen; hohe und kritische Containerfunde blockieren die Veröffentlichung                                |
| CodeQL                         | Go, JavaScript/TypeScript, Python und GitHub Actions                                                                                                                               |
| Dependency review / Dependabot | Abhängigkeitsänderungen in PRs und regelmäßige Updates für Go, npm, Docker und Actions                                                                                             |
| Publish container / Release    | Veröffentlichung erst nach erfolgreichen Prüfungen; Multiarch-Image, SBOM und Herkunftsnachweis, bei Versions-Tags zusätzlich GitHub Release                                       |
| Nightly recovery QA            | Isolierte Compose-Installation, Migrationen, echte Server-Zeiterfassung, Neustarts und Wiederherstellung von Datenbank und privaten Dateien                                        |
| GHCR cleanup                   | Manuelle Bereinigung alter, unreferenzierter Images; standardmäßig als Probelauf                                                                                                   |

Die publizierten Regressionen liegen in `internal/app/*_test.go` und `web/test/`, soweit sie in [.gitignore](.gitignore) ausdrücklich freigegeben sind. Weitere lokale Tests, interne Dokumentation, Screenshots und generierte Prüfberichte werden nicht veröffentlicht. Der genaue CI-Umfang ist in den Workflows nachlesbar.

Mobile Oberflächen werden mit virtuellen Handy-Ansichten geprüft. Eine Abnahme auf physischen Android- oder iPhone-Geräten ist damit nicht belegt.
