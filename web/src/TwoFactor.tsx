import { useState } from "react";
import { api } from "./api";
import type { Auth } from "./api";
import { useI18n } from "./i18n";
import { ActionForm, Field } from "./ui";
export function TwoFactor({
  auth,
  onRefresh,
}: {
  auth: Auth;
  onRefresh: () => Promise<void>;
}) {
  const { t } = useI18n();
  const [setup, setSetup] = useState<{ secret: string; otpauth_url: string }>();
  const [codes, setCodes] = useState<string[]>();
  return (
    <section className="panel">
      <h2>{t("twoFactor")}</h2>
      {codes ? (
        <>
          <p>{t("recoveryHelp")}</p>
          <h3>{t("recoveryCodes")}</h3>
          <div className="recovery-codes">
            {codes.map((code) => (
              <code key={code}>{code}</code>
            ))}
          </div>
          <button
            className="button secondary"
            onClick={() => setCodes(undefined)}
          >
            {t("close")}
          </button>
        </>
      ) : !auth.totp_available ? (
        <p>{t("mfaUnavailable")}</p>
      ) : setup ? (
        <>
          <p>{t("mfaInstructions")}</p>
          <Field label={t("mfaSecret")}>
            <input
              readOnly
              value={setup.secret}
              autoComplete="off"
              spellCheck={false}
            />
          </Field>
          <ActionForm
            label={t("confirmMfa")}
            onSubmit={async (form) => {
              const result = await api<{ recovery_codes: string[] }>(
                "/me/totp/confirm",
                "POST",
                { code: form.get("code") },
              );
              setCodes(result.recovery_codes);
              setSetup(undefined);
              await onRefresh();
            }}
          >
            <Field label={t("authCode")}>
              <input
                required
                name="code"
                autoComplete="one-time-code"
                inputMode="numeric"
                pattern="[0-9]{6}"
              />
            </Field>
          </ActionForm>
        </>
      ) : auth.user.totp_enabled ? (
        <ActionForm
          label={t("disableMfa")}
          onSubmit={async (form) => {
            await api("/me/totp/disable", "POST", {
              password: form.get("password"),
              code: form.get("code"),
            });
            await onRefresh();
          }}
        >
          <Field label={t("password")}>
            <input
              required
              type="password"
              name="password"
              autoComplete="current-password"
            />
          </Field>
          <Field label={t("authCode")}>
            <input required name="code" autoComplete="one-time-code" />
          </Field>
        </ActionForm>
      ) : (
        <ActionForm
          label={t("enableMfa")}
          onSubmit={async (form) =>
            setSetup(
              await api("/me/totp/enroll", "POST", {
                password: form.get("password"),
              }),
            )
          }
        >
          <Field label={t("password")}>
            <input
              required
              type="password"
              name="password"
              autoComplete="current-password"
            />
          </Field>
        </ActionForm>
      )}
    </section>
  );
}
