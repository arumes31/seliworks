import { useState } from "react";
import type { Auth, List } from "./api";
import { api } from "./api";
import { useData } from "./hooks";
import { useI18n } from "./i18n";
import { ActionForm, ErrorNotice, Field, Modal } from "./ui";
import { passkeysSupported, registerPasskey } from "./webauthn-client";

interface Passkey {
  id: string;
  name: string;
  created_at: string;
  last_used_at: string | null;
}
export function PasskeyAuthorizationFields({ mfa }: { mfa: boolean }) {
  const { t } = useI18n();
  return (
    <>
      <Field label={t("currentPassword")}>
        <input
          name="password"
          type="password"
          autoComplete="current-password"
          required
          maxLength={256}
        />
      </Field>
      {mfa && (
        <Field label={t("mfa")}>
          <input
            name="totp_code"
            autoComplete="one-time-code"
            required
            maxLength={64}
          />
        </Field>
      )}
    </>
  );
}
export function Passkeys({
  auth,
  onRefresh,
}: {
  auth: Auth;
  onRefresh: () => Promise<void>;
}) {
  const { t, date } = useI18n();
  const result = useData<List<Passkey>>("/me/passkeys");
  const [adding, setAdding] = useState(false);
  const [removing, setRemoving] = useState<Passkey>();
  const [busy, setBusy] = useState(false);
  async function refreshed() {
    await result.refresh();
    await onRefresh();
  }
  return (
    <section className="panel">
      <h2>{t("passkeys")}</h2>
      <p>{t("passkeysDescription")}</p>
      {result.error !== undefined && (
        <ErrorNotice error={result.error} retry={() => void result.refresh()} />
      )}
      {result.data?.items.map((key) => (
        <div className="separated" key={key.id}>
          <strong>{key.name}</strong>
          <p className="muted">
            {t("passkeyCreated")}: {date(key.created_at)} ·{" "}
            {t("passkeyLastUsed")}:{" "}
            {key.last_used_at ? date(key.last_used_at) : t("passkeyNever")}
          </p>
          <button
            className="button secondary danger-text"
            onClick={() => setRemoving(key)}
          >
            {t("removePasskey")}
          </button>
        </div>
      ))}
      {result.data?.total === 0 && <p>{t("noPasskeys")}</p>}
      {passkeysSupported() ? (
        <button className="button primary" onClick={() => setAdding(true)}>
          {t("addPasskey")}
        </button>
      ) : (
        <p className="notice">{t("passkeyUnsupported")}</p>
      )}
      {adding && (
        <Modal
          title={t("addPasskey")}
          busy={busy}
          onClose={() => setAdding(false)}
        >
          <p>{t("passkeySetupHint")}</p>
          <ActionForm
            label={t("addPasskey")}
            onSubmit={async (form) => {
              setBusy(true);
              try {
                await registerPasskey(form);
                setAdding(false);
                await refreshed();
              } finally {
                setBusy(false);
              }
            }}
          >
            <Field label={t("passkeyName")}>
              <input
                name="name"
                required
                maxLength={100}
                placeholder={t("passkeyNameExample")}
              />
            </Field>
            <PasskeyAuthorizationFields mfa={!!auth.user.totp_enabled} />
          </ActionForm>
        </Modal>
      )}
      {removing && (
        <Modal
          title={t("removePasskey")}
          busy={busy}
          onClose={() => setRemoving(undefined)}
        >
          <p>
            {t("removePasskeyConfirm")} <strong>{removing.name}</strong>
          </p>
          <ActionForm
            label={t("removePasskey")}
            secondary={
              <button
                type="button"
                className="button secondary"
                disabled={busy}
                onClick={() => setRemoving(undefined)}
              >
                {t("cancel")}
              </button>
            }
            onSubmit={async (form) => {
              setBusy(true);
              try {
                await api(`/me/passkeys/${removing.id}`, "DELETE", {
                  password: form.get("password"),
                  totp_code: form.get("totp_code") ?? "",
                });
                setRemoving(undefined);
                await refreshed();
              } finally {
                setBusy(false);
              }
            }}
          >
            <PasskeyAuthorizationFields mfa={!!auth.user.totp_enabled} />
          </ActionForm>
        </Modal>
      )}
    </section>
  );
}

export function PasskeyPrompt({
  auth,
  onSetup,
}: {
  auth: Auth;
  onSetup: () => void;
}) {
  const { t } = useI18n();
  const [dismissed, setDismissed] = useState(false);
  if (auth.user.passkey_count !== 0 || dismissed || !passkeysSupported())
    return null;
  return (
    <aside className="notice" aria-label={t("passkeys")}>
      <strong>{t("passkeyPrompt")}</strong>
      <p>{t("passkeysDescription")}</p>
      <div className="form-actions">
        <button className="button secondary" onClick={() => setDismissed(true)}>
          {t("later")}
        </button>
        <button className="button primary" onClick={onSetup}>
          {t("setupPasskey")}
        </button>
      </div>
    </aside>
  );
}
