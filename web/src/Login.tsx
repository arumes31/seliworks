import { useEffect, useId, useRef, useState } from "react";

import { ArrowRight, Eye, EyeOff, Leaf, ShieldCheck } from "lucide-react";

import type { Auth } from "./api";

import { api, APIError } from "./api";

import { useI18n } from "./i18n";

import { ActionForm, Brand, Field } from "./ui";

export function Login({
  onLogin,
  onLanguage,
}: {
  onLogin: (auth: Auth) => void;
  onLanguage: (lang: "en" | "de") => void;
}) {
  const { t, lang } = useI18n();
  const [show, setShow] = useState(false);
  const [mfaRequired, setMfaRequired] = useState(false);
  const codeInput = useRef<HTMLInputElement>(null);
  const credentialsVersion = useRef(0);
  const passwordId = useId();
  useEffect(() => {
    if (mfaRequired) codeInput.current?.focus();
  }, [mfaRequired]);
  function credentialsChanged() {
    credentialsVersion.current++;
    setMfaRequired(false);
  }
  return (
    <main className="login">
      <section className="login-art">
        <Brand />
        <div className="sun-scene" aria-hidden="true">
          <div className="sun" />
          <svg viewBox="0 0 900 600" preserveAspectRatio="xMidYMid slice">
            <path d="M-100 420Q200 170 1000 430M-100 450Q200 220 1000 460M-100 480Q200 270 1000 490M-100 510Q200 320 1000 520M-100 540Q200 370 1000 550M-100 570Q200 420 1000 580M-100 600Q200 470 1000 610M-100 630Q200 520 1000 640" />
            <path d="M400 300Q500 400 600 700M500 310Q600 420 700 700M600 330Q700 450 800 700M700 360Q800 480 900 700" />
          </svg>
        </div>
        <div className="login-message">
          <Leaf size={28} />
          <h1>{t("welcome")}</h1>
          <p>{t("loginSubtitle")}</p>
        </div>
        <span className="login-foot">
          SeliWorks · {new Date().getFullYear()}
        </span>
      </section>
      <section className="login-form">
        <div className="login-language">
          <button
            className={lang === "en" ? "selected" : ""}
            onClick={() => onLanguage("en")}
          >
            EN
          </button>
          <button
            className={lang === "de" ? "selected" : ""}
            onClick={() => onLanguage("de")}
          >
            DE
          </button>
        </div>
        <div className="login-form-inner">
          <div className="mobile-brand">
            <Brand />
          </div>
          <h2>{t("loginTitle")}</h2>
          <p className="muted">{t("loginIntro")}</p>
          <ActionForm
            label={t("signIn")}
            clearErrorOnInput
            onSubmit={async (form) => {
              const version = credentialsVersion.current;
              try {
                const auth = await api<Auth>("/auth/login", "POST", {
                  email: form.get("email"),
                  password: form.get("password"),
                  totp_code: form.get("totp_code") ?? "",
                });
                onLogin(auth);
              } catch (error) {
                // Ignore challenges for credentials edited while the request was pending.
                if (version !== credentialsVersion.current) return;
                if (error instanceof APIError) {
                  if (error.code === "MFA_REQUIRED") {
                    setMfaRequired(true);
                    codeInput.current?.focus();
                  } else if (error.code === "INVALID_CREDENTIALS") {
                    setMfaRequired(false);
                  }
                }
                throw error;
              }
            }}
          >
            <Field label={t("email")}>
              <input
                type="email"
                name="email"
                autoComplete="username"
                required
                autoFocus
                maxLength={254}
                onInput={credentialsChanged}
              />
            </Field>
            <Field label={t("password")} controlId={passwordId}>
              <div className="password-field">
                <input
                  id={passwordId}
                  type={show ? "text" : "password"}
                  name="password"
                  autoComplete="current-password"
                  required
                  maxLength={256}
                  onInput={credentialsChanged}
                />
                <button
                  type="button"
                  className="icon-button"
                  onClick={() => setShow(!show)}
                  aria-label={t(show ? "hidePassword" : "showPassword")}
                >
                  {show ? <EyeOff size={20} /> : <Eye size={20} />}
                </button>
              </div>
            </Field>
            {mfaRequired && (
              <Field label={t("mfa")}>
                <input
                  ref={codeInput}
                  name="totp_code"
                  autoComplete="one-time-code"
                  required
                  maxLength={64}
                />
              </Field>
            )}
          </ActionForm>
          <p className="login-help">{t("loginHelp")}</p>
          <div className="secure-note">
            <ShieldCheck size={17} />
            {t("safety")}
            <ArrowRight size={16} />
          </div>
        </div>
      </section>
    </main>
  );
}
