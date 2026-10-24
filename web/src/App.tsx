import { useCallback, useEffect, useRef, useState } from "react";

import {
  Activity,
  BarChart3,
  Building2,
  Clock3,
  FileCheck2,
  FileText,
  History as HistoryIcon,
  LayoutDashboard,
  Menu,
  Settings as SettingsIcon,
  ShieldCheck,
  Tags,
  UserRound,
  Users,
  X,
} from "lucide-react";

import { api, APIError, decimalMoney, setCSRF } from "./api";

import type { Auth, Entity, List, User } from "./api";

import { I18n, de, en } from "./i18n";

import type { Key } from "./i18n";

import { Login } from "./Login";
import { Setup } from "./Setup";

import { History, Worker } from "./Worker";

import { Profile } from "./Profile";

import type { InstallPrompt } from "./Profile";

import { Entries, Management, Overview } from "./Admin";
import { Settings } from "./Settings";

import { Invoices, Rates, Reports } from "./Billing";

import { Brand, ErrorNotice, Loading } from "./ui";

const adminNav = [
  ["overview", LayoutDashboard],
  ["entries", FileCheck2],
  ["workers", Users],
  ["farms", Building2],
  ["activities", Activity],
  ["rates", Tags],
  ["reports", BarChart3],
  ["invoices", FileText],
  ["settings", SettingsIcon],
] as const;

function initialRoute() {
  return window.location.hash.slice(1) || "";
}

export default function App() {
  const [auth, setAuth] = useState<Auth | null>();
  const [error, setError] = useState<unknown>();
  const [lang, setLang] = useState<"en" | "de">(() =>
    localStorage.getItem("seliworks:language") === "en" ? "en" : "de",
  );
  const [route, setRoute] = useState(initialRoute);
  const [mobileOpen, setMobileOpen] = useState(false);
  const [mobile, setMobile] = useState(
    () => matchMedia("(max-width:700px)").matches,
  );
  useEffect(() => {
    const media = matchMedia("(max-width:700px)");
    const handler = () => setMobile(media.matches);
    media.addEventListener("change", handler);
    return () => media.removeEventListener("change", handler);
  }, []);
  const [farms, setFarms] = useState<Entity[]>([]);
  const [activities, setActivities] = useState<Entity[]>([]);
  const [workers, setWorkers] = useState<Entity[]>([]);
  const [loadError, setLoadError] = useState<unknown>();
  const [preferenceError, setPreferenceError] = useState<unknown>();
  const entityGeneration = useRef(0);
  const [install, setInstall] = useState<InstallPrompt>();
  const [registration, setRegistration] = useState<ServiceWorkerRegistration>();
  const [update, setUpdate] = useState(false);

  const t = (key: Key): string => (lang === "de" ? de : en)[key];
  const locale = lang === "de" ? "de-AT" : "en-GB";
  const zone = auth?.organization.timezone || "Europe/Vienna";
  const currency = auth?.organization.currency || "EUR";
  const date = (value: string) =>
    value
      ? new Intl.DateTimeFormat(locale, {
          timeZone: zone,
          dateStyle: "medium",
          timeStyle: "short",
        }).format(new Date(value))
      : "—";
  const hours = (us: number) =>
    `${new Intl.NumberFormat(locale, { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format((us || 0) / 3600e6)} h`;
  const money = (value: string) => decimalMoney(value, currency, locale);

  const acceptAuth = useCallback((value: Auth) => {
    setCSRF(value.csrf_token);
    setAuth(value);
    setLang(value.user.language);
    localStorage.setItem("seliworks:language", value.user.language);
  }, []);

  const bootstrap = useCallback(async () => {
    setError(undefined);
    try {
      acceptAuth(await api<Auth>("/me"));
    } catch (err) {
      if (err instanceof APIError && err.status === 401) setAuth(null);
      else setError(err);
    }
  }, [acceptAuth]);

  useEffect(() => {
    void bootstrap();
  }, [bootstrap]);

  const loadEntities = useCallback(async () => {
    if (!auth) return;
    const generation = ++entityGeneration.current;
    try {
      const values = await Promise.all([
        api<List<Entity>>("/farms"),
        api<List<Entity>>("/activities"),
        auth.user.is_admin
          ? api<List<Entity>>("/admin/workers")
          : Promise.resolve({ items: [], total: 0 }),
      ]);
      if (generation !== entityGeneration.current) return;
      setFarms(values[0].items);
      setActivities(values[1].items);
      setWorkers(values[2].items);
      setLoadError(undefined);
    } catch (err) {
      if (generation === entityGeneration.current) setLoadError(err);
    }
  }, [auth]);

  useEffect(() => {
    void loadEntities();
    return () => {
      entityGeneration.current++;
    };
  }, [loadEntities]);

  useEffect(() => {
    const recover = () => {
      // Refresh even when the interrupted request has not rejected yet.
      if (!document.hidden) void loadEntities();
    };
    window.addEventListener("online", recover);
    window.addEventListener("focus", recover);
    document.addEventListener("visibilitychange", recover);
    return () => {
      window.removeEventListener("online", recover);
      window.removeEventListener("focus", recover);
      document.removeEventListener("visibilitychange", recover);
    };
  }, [loadEntities]);

  useEffect(() => {
    const handler = () => setRoute(initialRoute());
    window.addEventListener("hashchange", handler);
    return () => window.removeEventListener("hashchange", handler);
  }, []);

  useEffect(() => {
    document.documentElement.lang = lang;
    const theme = auth?.user.theme ?? "system";
    document.documentElement.dataset.theme = theme;
    localStorage.setItem("seliworks:language", lang);
  }, [lang, auth?.user.theme]);

  useEffect(() => {
    const beforeUnload = (event: BeforeUnloadEvent) => {
      if (
        document.documentElement.dataset.allowReload !== "true" &&
        document.querySelector('form[data-dirty="true"]')
      ) {
        event.preventDefault();
        event.returnValue = "";
      }
    };
    window.addEventListener("beforeunload", beforeUnload);
    return () => window.removeEventListener("beforeunload", beforeUnload);
  }, []);

  useEffect(() => {
    const installHandler = (event: Event) => {
      event.preventDefault();
      setInstall(event as InstallPrompt);
    };
    window.addEventListener("beforeinstallprompt", installHandler);
    if ("serviceWorker" in navigator) {
      void navigator.serviceWorker
        .register("/sw.js")
        .then((reg) => {
          setRegistration(reg);
          if (reg.waiting) setUpdate(true);
          reg.addEventListener("updatefound", () => {
            const worker = reg.installing;
            worker?.addEventListener("statechange", () => {
              if (
                worker.state === "installed" &&
                navigator.serviceWorker.controller
              )
                setUpdate(true);
            });
          });
        })
        .catch(() => {
          /* Installation remains optional; all work requires the server. */
        });
    }
    return () =>
      window.removeEventListener("beforeinstallprompt", installHandler);
  }, []);

  function navigate(next: string) {
    window.location.hash = next;
    setRoute(next);
    setMobileOpen(false);
    window.scrollTo({ top: 0 });
  }

  async function logout() {
    await api("/auth/logout", "POST", {});
    clearAccount();
  }

  function clearAccount() {
    setCSRF("");
    for (const storage of [localStorage, sessionStorage]) {
      for (let i = storage.length - 1; i >= 0; i--) {
        const key = storage.key(i);
        if (
          key?.startsWith(`seliworks:${auth!.user.id}:`) ||
          key === `seliworks:farm:${auth!.user.id}`
        )
          storage.removeItem(key);
      }
    }
    setAuth(null);
    setLoadError(undefined);
    setPreferenceError(undefined);
    setFarms([]);
    setActivities([]);
    setWorkers([]);
    navigate("");
  }

  useEffect(() => {
    const handler = () => {
      document.documentElement.dataset.hidden = String(document.hidden);
    };
    document.addEventListener("visibilitychange", handler);
    return () => document.removeEventListener("visibilitychange", handler);
  }, []);

  function updateUser(user: User) {
    if (auth) {
      setAuth({ ...auth, user });
      setLang(user.language);
    }
  }

  const current = route || (auth?.user.is_worker ? "work" : "overview");
  const isAdmin =
    auth?.user.is_admin && adminNav.some(([key]) => key === current);
  const navWorker = [
    ["work", Clock3],
    ["history", HistoryIcon],
    ["profile", UserRound],
  ] as const;

  let content;
  if (auth) {
    if (current === "work" && auth.user.is_worker)
      content = (
        <Worker user={auth.user} farms={farms} activities={activities} />
      );
    else if (current === "history" && auth.user.is_worker)
      content = <History activities={activities} userID={auth.user.id} />;
    else if (current === "profile")
      content = (
        <Profile
          auth={auth}
          onUpdate={updateUser}
          onLogout={logout}
          onPasswordChanged={clearAccount}
          install={install}
          onRefresh={bootstrap}
        />
      );
    else if (auth.user.is_admin) {
      if (current === "overview")
        content = (
          <Overview
            organization={auth.organization}
            onEntries={() => navigate("entries")}
          />
        );
      else if (current === "entries")
        content = (
          <Entries farms={farms} activities={activities} workers={workers} />
        );
      else if (["workers", "farms", "activities"].includes(current))
        content = (
          <Management
            key={current}
            kind={current as "workers" | "farms" | "activities"}
            farms={farms}
            onChanged={() => void loadEntities()}
          />
        );
      else if (current === "rates")
        content = (
          <Rates workers={workers} farms={farms} activities={activities} />
        );
      else if (current === "reports") content = <Reports />;
      else if (current === "invoices")
        content = (
          <Invoices
            farms={farms}
            smtpAvailable={auth.smtp_available ?? false}
          />
        );
      else if (current === "settings")
        content = <Settings onChanged={() => void bootstrap()} />;
      else
        content = (
          <Overview
            organization={auth.organization}
            onEntries={() => navigate("entries")}
          />
        );
    }
  }

  return (
    <I18n.Provider
      value={{ lang, t, date, hours, money, timezone: zone, currency }}
    >
      {route.startsWith("setup/") ? (
        <Setup
          token={route.slice(6)}
          onDone={() => {
            navigate("");
            setAuth(null);
          }}
        />
      ) : auth === undefined ? (
        <main className="startup">
          <Brand />
          {error !== undefined ? (
            <ErrorNotice error={error} retry={() => void bootstrap()} />
          ) : (
            <Loading />
          )}
        </main>
      ) : auth === null ? (
        <Login onLogin={acceptAuth} onLanguage={setLang} />
      ) : (
        <div className={`app-shell ${isAdmin ? "admin-mode" : "worker-mode"}`}>
          <a className="skip-link" href="#main">
            {t("work")}
          </a>
          <aside
            className={`sidebar ${mobileOpen ? "open" : ""}`}
            inert={mobile && !mobileOpen}
          >
            <div className="sidebar-brand">
              <Brand />
              <button
                className="icon-button mobile-close"
                aria-label={t("close")}
                onClick={() => setMobileOpen(false)}
              >
                <X />
              </button>
            </div>
            {auth.user.is_worker && (
              <nav aria-label={t("workerView")} className="worker-desktop-nav">
                {navWorker.map(([key, Icon]) => (
                  <button
                    className={
                      current === key ? "nav-item selected" : "nav-item"
                    }
                    key={key}
                    onClick={() => navigate(key)}
                  >
                    <Icon size={20} />
                    {t(key)}
                  </button>
                ))}
              </nav>
            )}
            {auth.user.is_admin && (
              <>
                <div className="nav-label">
                  <ShieldCheck size={15} />
                  {t("admin")}
                </div>
                <nav aria-label={t("admin")}>
                  {adminNav.map(([key, Icon]) => (
                    <button
                      key={key}
                      className={
                        current === key ? "nav-item selected" : "nav-item"
                      }
                      onClick={() => navigate(key)}
                    >
                      <Icon size={20} />
                      {t(key)}
                    </button>
                  ))}
                </nav>
              </>
            )}
            <div className="sidebar-bottom">
              <div className="organization-chip">
                <Brand compact />
                <span>{auth.organization.name}</span>
              </div>
              <button
                className="account-button"
                onClick={() => navigate("profile")}
              >
                <span className="avatar">
                  {auth.user.name.slice(0, 2).toUpperCase()}
                </span>
                <span>
                  <strong>{auth.user.name}</strong>
                  <small>
                    {auth.user.is_admin
                      ? t("administrator")
                      : t("workerPermission")}
                  </small>
                </span>
                <SettingsIcon size={17} />
              </button>
            </div>
          </aside>
          {mobileOpen && (
            <button
              className="menu-scrim"
              aria-label={t("close")}
              onClick={() => setMobileOpen(false)}
            />
          )}
          <div className="workspace">
            <header className="topbar">
              <button
                className="icon-button mobile-menu"
                onClick={() => setMobileOpen(true)}
                aria-label={t("admin")}
              >
                <Menu />
              </button>
              <span className="topbar-context">
                {t(isAdmin ? "team" : "workerView")}
              </span>
              <div className="topbar-right">
                <span className="timezone-chip">{zone}</span>
                <button
                  className="language-button"
                  onClick={() => {
                    const language = lang === "en" ? "de" : "en";
                    setPreferenceError(undefined);
                    void api<User | { user: User }>("/me", "PATCH", {
                      language,
                      theme: auth.user.theme,
                    })
                      .then((result) =>
                        updateUser("user" in result ? result.user : result),
                      )
                      .catch(setPreferenceError);
                  }}
                >
                  {lang.toUpperCase()}
                </button>
                <button
                  className="avatar topbar-avatar"
                  onClick={() => navigate("profile")}
                  aria-label={t("profile")}
                >
                  {auth.user.name.slice(0, 2).toUpperCase()}
                </button>
              </div>
            </header>
            <main id="main" className="main-content">
              {update && (
                <div className="notice update-notice">
                  <span>{t("installReady")}</span>
                  <button
                    className="button secondary"
                    onClick={() => {
                      const waiting = registration?.waiting;
                      if (!waiting) return;
                      if (
                        document.querySelector('form[data-dirty="true"]') &&
                        !window.confirm(t("unsavedUpdate"))
                      )
                        return;
                      document.documentElement.dataset.allowReload = "true";
                      const reload = () => window.location.reload();
                      navigator.serviceWorker.addEventListener(
                        "controllerchange",
                        reload,
                        { once: true },
                      );
                      waiting.postMessage({
                        type: "SKIP_WAITING",
                      });
                    }}
                  >
                    {t("update")}
                  </button>
                </div>
              )}
              {loadError !== undefined && (
                <ErrorNotice
                  error={loadError}
                  retry={() => void loadEntities()}
                />
              )}{" "}
              {preferenceError !== undefined && (
                <ErrorNotice
                  error={preferenceError}
                  retry={() => {
                    void api<Auth>("/me")
                      .then((value) => {
                        acceptAuth(value);
                        setPreferenceError(undefined);
                      })
                      .catch(setPreferenceError);
                  }}
                />
              )}
              {content}
            </main>
          </div>
          {auth.user.is_worker && (
            <nav className="bottom-nav" aria-label={t("workerView")}>
              {navWorker.map(([key, Icon]) => (
                <button
                  key={key}
                  className={current === key ? "selected" : ""}
                  onClick={() => navigate(key)}
                >
                  <Icon size={22} />
                  <span>{t(key)}</span>
                </button>
              ))}
            </nav>
          )}
        </div>
      )}
    </I18n.Provider>
  );
}
