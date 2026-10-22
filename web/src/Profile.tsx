import { useState } from "react";

import { Download, LogOut, ShieldCheck, Smartphone } from "lucide-react";

import { api } from "./api";

import type { Auth, User } from "./api";

import { useI18n } from "./i18n";

import { TwoFactor } from "./TwoFactor";

import { ActionForm, ErrorNotice, Field, PageHeader } from "./ui";

export interface InstallPrompt extends Event {
  prompt: () => Promise<void>;
  userChoice: Promise<{ outcome: string }>;
}

export function Profile({
  auth,
  onUpdate,
  onLogout,
  install,
  onRefresh,
  onPasswordChanged,
}: {
  auth: Auth;
  onUpdate: (user: User) => void;
  onLogout: () => Promise<void>;
  install?: InstallPrompt;
  onRefresh: () => Promise<void>;
  onPasswordChanged: () => void;
}) {
  const { t } = useI18n();
  const [error, setError] = useState<unknown>();
  return (
    <>
      <PageHeader
        title={t("profile")}
        description={`${auth.user.name} · ${auth.user.email}`}
      />
      <div className="settings-grid">
        <section className="panel">
          <h2>{t("theme")}</h2>
          <ActionForm
            label={t("save")}
            onSubmit={async (form) => {
              const result = await api<User | { user: User }>("/me", "PATCH", {
                language: form.get("language"),
                theme: form.get("theme"),
              });
              onUpdate("user" in result ? result.user : result);
            }}
          >
            <Field label={t("language")}>
              <select name="language" defaultValue={auth.user.language}>
                <option value="en">English</option>
                <option value="de">Deutsch</option>
              </select>
            </Field>
            <Field label={t("theme")}>
              <select name="theme" defaultValue={auth.user.theme}>
                <option value="light">{t("light")}</option>
                <option value="dark">{t("dark")}</option>
                <option value="system">{t("system")}</option>
              </select>
            </Field>
          </ActionForm>
        </section>
        <section className="panel">
          <h2>
            <ShieldCheck size={21} />
            {t("security")}
          </h2>
          <ActionForm
            label={t("changePassword")}
            onSubmit={async (form) => {
              await api("/me/password", "POST", {
                current_password: form.get("current_password"),
                new_password: form.get("new_password"),
              });
              onPasswordChanged();
            }}
          >
            <Field label={t("currentPassword")}>
              <input
                type="password"
                name="current_password"
                autoComplete="current-password"
                required
              />
            </Field>
            <Field label={t("newPassword")}>
              <input
                type="password"
                name="new_password"
                autoComplete="new-password"
                required
                minLength={12}
                maxLength={256}
              />
            </Field>
          </ActionForm>
        </section>
        <TwoFactor auth={auth} onRefresh={onRefresh} />
        <section className="panel">
          <h2>
            <Smartphone size={21} />
            {t("installation")}
          </h2>
          <p>{t("installHelp")}</p>
          {install && (
            <button
              className="button secondary"
              onClick={() => void install.prompt()}
            >
              <Download size={18} />
              {t("install")}
            </button>
          )}
        </section>
        <section className="panel">
          <h2>{t("logout")}</h2>
          <p>{t("logoutHint")}</p>
          {error !== undefined && <ErrorNotice error={error} />}
          <button
            className="button secondary"
            onClick={() => {
              void onLogout().catch(setError);
            }}
          >
            <LogOut size={18} />
            {t("logout")}
          </button>
        </section>
      </div>
    </>
  );
}
