import { api } from "./api";
import { useI18n } from "./i18n";
import { ActionForm, Brand, Field } from "./ui";
export function Setup({
  token,
  onDone,
}: {
  token: string;
  onDone: () => void;
}) {
  const { t } = useI18n();
  return (
    <main className="startup setup">
      <Brand />
      <h1>{t("setupAccount")}</h1>
      <p>{t("setupHint")}</p>
      <ActionForm
        label={t("setupAccount")}
        onSubmit={async (form) => {
          if (form.get("password") !== form.get("repeat"))
            throw new Error(t("passwordMismatch"));
          await api("/auth/setup", "POST", {
            token,
            password: form.get("password"),
          });
          onDone();
        }}
      >
        <Field label={t("newPassword")}>
          <input
            name="password"
            type="password"
            autoComplete="new-password"
            required
            minLength={12}
            maxLength={256}
          />
        </Field>
        <Field label={t("repeatPassword")}>
          <input
            name="repeat"
            type="password"
            autoComplete="new-password"
            required
            minLength={12}
            maxLength={256}
          />
        </Field>
      </ActionForm>
    </main>
  );
}
