const de: Record<string, string> = {
  PASSKEY_INVALID:
    "Der Passkey konnte nicht bestätigt werden oder die Anfrage ist abgelaufen. Starte den Vorgang erneut.",
  PASSKEY_LIMIT:
    "Es sind bereits 20 Passkeys gespeichert. Entferne zuerst einen nicht mehr verwendeten Schlüssel.",
  PASSKEY_CANCELLED:
    "Passkey-Vorgang abgebrochen oder nicht verfügbar. Versuche es erneut oder verwende dein Passwort.",
  PASSKEY_EXISTS:
    "Dieser Passkey ist bereits eingerichtet. Wähle ein anderes Gerät oder einen anderen Schlüssel.",
  PASSKEY_UNSUPPORTED:
    "Dieser Browser unterstützt keine Passkeys. Verwende dein Passwort.",
  NETWORK_READ:
    "Die Daten konnten nicht geladen werden. Prüfe deine Verbindung und aktualisiere die Ansicht.",
  NETWORK_WRITE:
    "Die Verbindung wurde unterbrochen oder der Vorgang dauert zu lange. Der Erfolg ist nicht bestätigt; gleiche den Zustand vor einem erneuten Versuch ab.",
  INVALID_RESPONSE:
    "Der Server hat eine unlesbare Antwort geliefert. Aktualisiere die Ansicht und prüfe den Zustand vor einem erneuten Versuch.",
  AMBIGUOUS_LOCAL_TIME:
    "Diese Ortszeit kommt zweimal vor. Wähle ausdrücklich den UTC-Offset.",
  NONEXISTENT_LOCAL_TIME:
    "Diese Ortszeit existiert wegen der Zeitumstellung nicht. Wähle eine gültige Uhrzeit.",
  ACTIVITY_REQUIRED: "Wähle eine aktive Tätigkeit.",
  ALREADY_PAUSED: "Diese Arbeitszeit ist bereits pausiert.",
  ALREADY_RUNNING:
    "Eine Arbeitszeit läuft bereits. Aktualisiere die Ansicht, um sie wiederherzustellen.",
  AMOUNT_TOO_LARGE:
    "Der berechnete Betrag überschreitet den unterstützten Bereich. Prüfe Stundensatz und Zuschlag.",
  BILLING_LOCKED:
    "Storniere zuerst den Rechnungsentwurf oder berichtige die ausgestellte Rechnung über den Finanzablauf.",
  BREAK_CONFLICT:
    "Die Korrektur würde eine bestehende Pause ausschließen. Prüfe die Zeitpunkte und Pausen.",
  CLOCK_ANOMALY:
    "Die Datenbankuhr widerspricht den gespeicherten Zeitnachweisen. Die Änderung wurde nicht bestätigt; eine Administration muss die Zeiten prüfen.",
  CONFIRMATION_REQUIRED:
    "Prüfe die Vorschau und bestätige die Bereinigung ausdrücklich.",
  CONFLICT:
    "Eine andere Änderung steht im Konflikt mit diesem Vorgang. Aktualisiere die Ansicht und prüfe den Eintrag.",
  CSRF: "Aktualisiere deine Anmeldung vor dem Speichern.",
  DAY_TYPE_CONFLICT:
    "Urlaubsangabe und ausgewählter Tagestyp müssen übereinstimmen.",
  DAY_TYPE_REQUIRED:
    "Wähle einen normalen Arbeitstag, Urlaubstag oder Feiertag.",
  DUPLICATE_CONFIRMATION_REQUIRED:
    "Der Mailserver könnte diese Nachricht bereits angenommen haben. Bestätige vor einem erneuten Versuch das Risiko einer doppelten Zustellung.",
  EXCESS_PAYMENT: "Die Zahlung übersteigt den offenen Betrag.",
  FARM_NOT_ASSIGNED: "Wähle einen zugewiesenen aktiven Betrieb.",
  FORBIDDEN:
    "Du hast für diesen Vorgang keine Berechtigung. Prüfe Rolle und Kontostatus.",
  FUTURE_WORK: "Abgeschlossene Arbeit darf nicht in der Zukunft enden.",
  HOLIDAY_POLICY_REQUIRED:
    "Konfiguriere den Feiertagszuschlag und gib die betroffene Arbeit danach erneut frei.",
  IDEMPOTENCY_CONFLICT:
    "Dieser Befehlsschlüssel wurde bereits mit anderen Angaben verwendet. Gleiche den Vorgang ab.",
  IDEMPOTENCY_KEY:
    "Für diesen Vorgang ist ein gültiger Befehlsschlüssel erforderlich.",
  IDEMPOTENCY_REQUIRED:
    "Für diesen Vorgang ist ein gültiger Befehlsschlüssel erforderlich.",
  INTERNAL: "Ein unerwarteter Serverfehler ist aufgetreten.",
  INTERVAL_CONFLICT:
    "Dieser Zeitraum überschneidet sich mit einem vorhandenen Eintrag. Prüfe den Zeitraum vor dem Speichern.",
  INVALID_CODE: "Der Authentifizierungscode ist falsch.",
  INVALID_CREDENTIALS: "E-Mail-Adresse oder Passwort ist falsch.",
  INVALID_DAY_TYPE:
    "Wähle einen normalen Arbeitstag, Urlaubstag oder Feiertag.",
  INVALID_DEDUCTION:
    "Der zusätzliche Abzug muss zwischen null und der nach Pausen verbleibenden Arbeitszeit liegen.",
  INVALID_INVITATION: "Der Einrichtungslink ist ungültig oder abgelaufen.",
  INVALID_PASSWORD: "Das aktuelle Passwort ist falsch.",
  INVALID_STATE:
    "Dieser Vorgang ist im aktuellen Status nicht möglich. Aktualisiere die Ansicht und prüfe den Eintrag.",
  KEY_REUSE:
    "Dieser Befehlsschlüssel wurde bereits für andere Angaben verwendet.",
  LAST_ADMIN:
    "Mindestens ein aktives Administrationskonto muss erhalten bleiben.",
  MFA_ENABLED:
    "Deaktiviere die bestehende Authentifizierung, bevor du sie ersetzt.",
  MFA_REQUIRED:
    "Gib einen aktuellen Authentifizierungscode oder einen unbenutzten Wiederherstellungscode ein.",
  MFA_UNAVAILABLE:
    "Die Authentifizierung ist noch nicht verfügbar. Kontaktiere die Administration der Installation.",
  NO_ELIGIBLE_WORK:
    "In diesem Zeitraum gibt es keine freigegebene verrechenbare Arbeit.",
  NOT_FOUND: "Der gesuchte Eintrag ist nicht vorhanden oder nicht zugänglich.",
  NOT_ISSUED:
    "Stelle die Rechnung aus, bevor du ihr gespeichertes PDF herunterlädst.",
  NOT_READY: "Die Datenbank ist derzeit nicht verfügbar.",
  PASSWORD_POLICY: "Verwende ein Passwort mit 12 bis 256 Zeichen.",
  PDF_UNAVAILABLE:
    "Das gespeicherte Rechnungs-PDF ist nicht verfügbar oder für den Mailversand zu groß. Prüfe die Dokumentensicherung.",
  PRICING_REVIEW_REQUIRED:
    "Die Freigaben verwenden unterschiedliche Währungen oder Steuerbehandlungen. Wähle getrennte Leistungszeiträume.",
  RATE_LIMIT: "Zu viele Versuche. Versuche es in 15 Minuten erneut.",
  RATE_REQUIRED:
    "Ergänze den fehlenden Stundensatz und gib die Arbeit ausdrücklich erneut frei.",
  REASON_REQUIRED:
    "Für diesen Vorgang ist eine nachvollziehbare Begründung erforderlich.",
  RECIPIENT_REQUIRED:
    "Die gespeicherten Kundendaten der Rechnung benötigen eine gültige E-Mail-Adresse.",
  SCHEMA_UPGRADE_REQUIRED:
    "Die Datenbankmigrationen müssen vor der Nutzung der Anwendung ausgeführt werden.",
  SMTP_NOT_CONFIGURED:
    "Konfiguriere vor dem Rechnungsversand einen Mailserver und eine gültige Absenderadresse.",
  STALE_DRAFT:
    "Die zugrunde liegende Arbeit hat sich geändert. Storniere den Entwurf und prüfe einen neuen.",
  STATE_CONFLICT:
    "Der Status hat sich geändert oder erlaubt diesen Vorgang nicht. Aktualisiere die Ansicht und prüfe den Eintrag.",
  TAX_SETUP_REQUIRED:
    "Bestätige vor der Verrechnung die steuerliche Behandlung der Organisation.",
  UNAUTHENTICATED: "Bitte melde dich erneut an.",
  UNAVAILABLE:
    "Der Vorgang konnte nicht bestätigt werden. Stelle die Verbindung wieder her und gleiche den Zustand vor einem erneuten Versuch ab.",
  VACATION_POLICY_REQUIRED:
    "Konfiguriere den Urlaubstagszuschlag und gib die betroffene Arbeit danach erneut frei.",
  VALIDATION: "Prüfe die Eingaben, Zeitintervalle und referenzierten Einträge.",
  VERSION_CONFLICT:
    "Dieser Eintrag wurde zwischenzeitlich geändert. Aktualisiere die Ansicht und prüfe ihn vor dem Speichern.",
  WORK_RESERVED:
    "Die geeignete Arbeit in diesem Zeitraum ist bereits reserviert oder verrechnet.",
};

const details: Record<string, string> = {
  "Pause duration must be between 1 and 1440 minutes":
    "Die Pausendauer muss zwischen 1 und 1440 Minuten liegen.",
  "Resume must identify the pause":
    "Zum Fortsetzen muss die betroffene Pause angegeben werden.",
  "Choose an activity and describe the work (up to 4000 characters)":
    "Wähle eine Tätigkeit und beschreibe die Arbeit mit höchstens 4000 Zeichen.",
  "Payment must be a positive decimal with at most two decimal places":
    "Die Zahlung muss ein positiver Betrag mit höchstens zwei Nachkommastellen sein.",
  "Payment requires a valid date and reference of at most 500 characters":
    "Gib ein gültiges Zahlungsdatum und eine Referenz mit höchstens 500 Zeichen ein.",
  "Rate requires a nonnegative decimal with at most four decimal places":
    "Der Stundensatz muss mindestens null betragen und darf höchstens vier Nachkommastellen haben.",
  "Effective end must follow start":
    "Das Gültigkeitsende muss nach dem Beginn liegen.",
  "Effective start requires an explicit timezone offset":
    "Der Gültigkeitsbeginn benötigt eine eindeutige Zeitzonenangabe.",
  "Activity rates require both a worker and farm":
    "Tätigkeitssätze benötigen eine Person und einen Betrieb.",
  "Select a positive date period of at most ten years; end date is exclusive":
    "Wähle einen Zeitraum von höchstens zehn Jahren mit Ende nach Beginn. Das Enddatum ist ausgeschlossen.",
  "Check organization settings, timezone and invoice fields":
    "Prüfe Organisationseinstellungen, Zeitzone und Rechnungsangaben.",
  "Check tax identifier, invoice footer and invoice layout":
    "Prüfe Steuerkennung, Rechnungsfußtext und Darstellungsart.",
  "Use a security retention period from 1 to 3650 days and policy notes up to 2000 characters":
    "Wähle eine Sicherheitsaufbewahrung von 1 bis 3650 Tagen und Richtliniennotizen mit höchstens 2000 Zeichen.",
  "Use a nonnegative decimal with at most four fractional digits":
    "Verwende einen Wert ab null mit höchstens vier Nachkommastellen und einem Dezimalpunkt.",
  "Only running work can be paused":
    "Nur laufende Arbeit kann pausiert werden.",
  "This work session has already stopped":
    "Diese Arbeitszeit wurde bereits gestoppt.",
  "Stop the running session explicitly before editing its completed evidence":
    "Stoppe die laufende Arbeitszeit ausdrücklich, bevor du den abgeschlossenen Nachweis bearbeitest.",
  "Only draft invoices can be issued":
    "Nur Rechnungsentwürfe können ausgestellt werden.",
  "Payments require an issued invoice":
    "Zahlungen können nur für ausgestellte Rechnungen erfasst werden.",
};

export function localizedAPIError(
  code: string,
  message: string,
  language: string,
) {
  return language === "de"
    ? (details[message] ?? de[code] ?? de.UNAVAILABLE)
    : message;
}

export function hasGermanErrorCode(code: string) {
  return code in de;
}
