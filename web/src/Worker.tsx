import { useCallback, useEffect, useRef, useState } from "react";

import {
  ArrowRight,
  Clock3,
  Play,
  Pause,
  RefreshCw,
  Square,
  WifiOff,
} from "lucide-react";

import { api, APIError, elapsed } from "./api";

import type { Entity, List, User, Work } from "./api";

import { useData, useOnline } from "./hooks";

import { useI18n } from "./i18n";

import {
  ActionForm,
  Empty,
  ErrorNotice,
  Field,
  Loading,
  Modal,
  PageHeader,
  Status,
} from "./ui";

import { WorkDetails } from "./WorkDetails";

interface Summary {
  today_us: number;
  month_us: number;
  year_us: number;
}

interface Command {
  path: string;
  body: {
    farm_id?: string;
    duration_minutes?: number | null;
    pause_id?: string;
  };
  key: string;
}

export function Worker({
  user,
  activities,
  farms,
}: {
  user: User;
  activities: Entity[];
  farms: Entity[];
}) {
  const { t, hours, date } = useI18n();
  const online = useOnline();
  const [active, setActive] = useState<Work | null>();
  const [summary, setSummary] = useState<Summary>();
  const [pending, setPending] = useState<Work[]>([]);
  const [selected, setSelected] = useState<Work>();
  const [farm, setFarm] = useState(
    () => localStorage.getItem(`seliworks:farm:${user.id}`) || "",
  );
  const [error, setError] = useState<unknown>();
  const [syncTime, setSyncTime] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [pauseOpen, setPauseOpen] = useState(false);
  const [pauseMode, setPauseMode] = useState("timed");
  const [command, setCommand] = useState<Command | undefined>(() => {
    try {
      const raw = sessionStorage.getItem(
        "seliworks:" + user.id + ":pending-command",
      );
      if (!raw) return undefined;
      const value = JSON.parse(raw) as Command;
      return value.key && value.path && value.body ? value : undefined;
    } catch {
      return undefined;
    }
  });
  const [reconciled, setReconciled] = useState(false);
  const [tick, setTick] = useState(0);
  const sample = useRef({ us: 0, monotonic: performance.now() });

  const synchronize = useCallback(async () => {
    try {
      const [session, totals, incomplete, rejected] = await Promise.all([
        api<Work | null>("/me/active-work-session"),
        api<Summary>("/me/summary"),
        api<List<Work>>(
          `/work-sessions?state=STOPPED_PENDING_DETAILS&page_size=100&worker_id=${user.id}`,
        ),
        api<List<Work>>(
          `/work-sessions?state=REJECTED&page_size=100&worker_id=${user.id}`,
        ),
      ]);
      setActive(session);
      setSummary(totals);
      setPending([...incomplete.items, ...rejected.items]);
      sample.current = {
        us: session?.elapsed_us ?? 0,
        monotonic: performance.now(),
      };
      setSyncTime(new Date().toISOString());
      setError(undefined);
      setReconciled(true);
    } catch (err) {
      setError(err);
    }
  }, [user.id]);

  useEffect(() => {
    const pause = active?.active_pause;
    if (!pause?.planned_stop || !active) return;
    const delay = Math.max(
      500,
      Date.parse(pause.planned_stop) - Date.parse(active.server_now) + 500,
    );
    const deadline = setTimeout(() => void synchronize(), delay);
    return () => clearTimeout(deadline);
  }, [active, synchronize]);

  useEffect(() => {
    void synchronize();
    const focus = () => {
      if (!document.hidden) void synchronize();
    };
    window.addEventListener("focus", focus);
    window.addEventListener("online", focus);
    document.addEventListener("visibilitychange", focus);
    const polling = setInterval(focus, 30000);
    const display = setInterval(() => setTick((v) => v + 1), 1000);
    return () => {
      window.removeEventListener("focus", focus);
      window.removeEventListener("online", focus);
      document.removeEventListener("visibilitychange", focus);
      clearInterval(polling);
      clearInterval(display);
    };
  }, [synchronize]);

  useEffect(() => {
    if (!farms.some((f) => f.id === farm && f.active))
      setFarm(farms.find((f) => f.active)?.id ?? "");
  }, [farms, farm]);

  async function execute(next: Command) {
    if (busy) return;
    setBusy(true);
    setCommand(next);
    sessionStorage.setItem(
      "seliworks:" + user.id + ":pending-command",
      JSON.stringify(next),
    );
    setReconciled(false);
    setError(undefined);
    try {
      const session = await api<Work>(next.path, "POST", next.body, next.key);
      setCommand(undefined);
      sessionStorage.removeItem("seliworks:" + user.id + ":pending-command");
      if (session.stopped_at) {
        setActive(null);
        setSelected(session);
      } else {
        setActive(session);
        sample.current = {
          us: session.elapsed_us,
          monotonic: performance.now(),
        };
      }
      await synchronize();
    } catch (err) {
      setError(err);
      if (err instanceof APIError && err.status < 500) {
        setCommand(undefined);
        sessionStorage.removeItem("seliworks:" + user.id + ":pending-command");
      }
      await synchronize();
      setError(err);
    } finally {
      setBusy(false);
    }
  }

  const displayUS = active
    ? sample.current.us +
      (active.active_pause
        ? 0
        : (performance.now() - sample.current.monotonic) * 1000)
    : 0;
  void tick;
  const isStale = !online || !!error;

  return (
    <>
      <PageHeader
        title={`${t("work")}, ${user.name.split(" ")[0]}`}
        action={
          <button
            className="icon-button"
            aria-label={t("sync")}
            onClick={() => void synchronize()}
          >
            <RefreshCw size={20} />
          </button>
        }
      />
      {(!online || error !== undefined) && (
        <div className="notice warning" role="status">
          <WifiOff size={18} />
          {!online ? t("offline") : t("stale")}
        </div>
      )}
      {error !== undefined && <ErrorNotice error={error} />}
      <div className="worker-layout">
        <section className={`work-timer ${active ? "is-running" : ""}`}>
          <div className="timer-top">
            <Clock3 size={22} />
            <span>
              {active
                ? isStale
                  ? t("stale")
                  : t(active.active_pause ? "paused" : "running")
                : t("ready")}
            </span>
            {active && <span className="live-dot" />}
          </div>
          {active === undefined ? (
            <Loading />
          ) : (
            <>
              <div
                className="timer"
                role="timer"
                aria-live="off"
                aria-label={t("duration")}
              >
                {elapsed(displayUS)}
              </div>
              {active ? (
                <div className="current-farm">
                  <strong>{active.farm_name}</strong>
                  <span>
                    {t("started")} {date(active.started_at)}
                  </span>
                </div>
              ) : (
                <Field label={t("farm")}>
                  <select
                    value={farm}
                    onChange={(e) => {
                      setFarm(e.target.value);
                      localStorage.setItem(
                        `seliworks:farm:${user.id}`,
                        e.target.value,
                      );
                    }}
                    disabled={busy || !!command}
                  >
                    {farms
                      .filter((f) => f.active)
                      .map((f) => (
                        <option value={f.id} key={f.id}>
                          {f.name}
                        </option>
                      ))}
                  </select>
                </Field>
              )}
              <p className="timer-hint">
                {t(
                  active
                    ? active.active_pause
                      ? "pauseHint"
                      : "runningHint"
                    : "readyHint",
                )}
              </p>
              {!farm && !active && <p className="notice">{t("noFarms")}</p>}
              {active?.active_pause?.planned_stop && (
                <p className="pause-countdown">
                  {t("pauseCountdown")}{" "}
                  <strong>
                    {elapsed(
                      Math.max(
                        0,
                        (Date.parse(active.active_pause.planned_stop) -
                          Date.parse(active.server_now)) *
                          1000 -
                          (performance.now() - sample.current.monotonic) * 1000,
                      ),
                    )}
                  </strong>
                </p>
              )}
              {active && (
                <button
                  className="button secondary pause-action"
                  disabled={busy || !online || !!command}
                  onClick={() => {
                    if (active.active_pause)
                      void execute({
                        path: `/work-sessions/${active.id}/resume`,
                        body: { pause_id: active.active_pause.id },
                        key: crypto.randomUUID(),
                      });
                    else setPauseOpen(true);
                  }}
                >
                  {active.active_pause ? (
                    <Play size={20} />
                  ) : (
                    <Pause size={20} />
                  )}{" "}
                  {t(active.active_pause ? "resume" : "pause")}
                </button>
              )}
              <button
                className={`button timer-action ${active ? "stop" : "primary"}`}
                disabled={busy || !online || !!command || (!active && !farm)}
                onClick={() =>
                  void execute({
                    path: active
                      ? `/work-sessions/${active.id}/stop`
                      : "/work-sessions/start",
                    body: active ? {} : { farm_id: farm },
                    key: crypto.randomUUID(),
                  })
                }
              >
                {active ? (
                  <Square size={24} fill="currentColor" />
                ) : (
                  <Play size={25} fill="currentColor" />
                )}
                {busy ? t("loading") : t(active ? "stop" : "start")}
              </button>
            </>
          )}
          {syncTime && (
            <p className="sync-time">
              {t("lastSync")} {date(syncTime)}
            </p>
          )}
          {command && (
            <div className="notice warning">
              <p>{t("unknown")}</p>
              <button
                className="button secondary"
                disabled={busy || !online || !reconciled}
                onClick={() => void execute(command)}
              >
                {t("retry")}
              </button>
            </div>
          )}
        </section>
        <aside className="work-aside">
          <section className="summary-panel">
            <h2>{t("overview")}</h2>
            {(["today", "month", "year"] as const).map((key) => (
              <div className="summary-line" key={key}>
                <span>{t(key)}</span>
                <strong>{summary ? hours(summary[`${key}_us`]) : "—"}</strong>
              </div>
            ))}
          </section>
          <section className="field-note">
            <svg viewBox="0 0 240 80" aria-hidden="true">
              <path d="M0 55Q70 5 240 55M0 66Q70 20 240 66M0 77Q70 35 240 77" />
              <circle cx="165" cy="22" r="14" />
            </svg>
            <p>{t("safety")}</p>
          </section>
        </aside>
      </div>
      {pending.length > 0 && (
        <section className="section-block">
          <div className="section-title">
            <h2>{t("pendingCount")}</h2>
            <span className="count">{pending.length}</span>
          </div>
          {pending.map((entry) => (
            <button
              key={entry.id}
              className="pending-row"
              onClick={() => setSelected(entry)}
            >
              <div>
                <strong>{entry.farm_name}</strong>
                <span>
                  {date(entry.started_at)} · {hours(entry.net_us)}
                </span>
              </div>
              <span>
                {t("complete")}
                <ArrowRight size={18} />
              </span>
            </button>
          ))}
        </section>
      )}
      {pauseOpen && active && (
        <Modal title={t("pauseMode")} onClose={() => setPauseOpen(false)}>
          <ActionForm
            label={t("confirmPause")}
            onSubmit={async (form) => {
              const next: Command = {
                path: `/work-sessions/${active.id}/pause`,
                body: {
                  duration_minutes:
                    pauseMode === "timed" ? Number(form.get("minutes")) : null,
                },
                key: crypto.randomUUID(),
              };
              setPauseOpen(false);
              await execute(next);
            }}
          >
            <label className="check">
              <input
                type="radio"
                name="pause_mode"
                value="timed"
                checked={pauseMode === "timed"}
                onChange={() => setPauseMode("timed")}
              />
              {t("pauseMinutes")}
            </label>
            {pauseMode === "timed" && (
              <Field label={t("minutes")}>
                <input
                  name="minutes"
                  type="number"
                  min={1}
                  max={1440}
                  step={1}
                  defaultValue={30}
                  required
                  inputMode="numeric"
                />
              </Field>
            )}
            <label className="check">
              <input
                type="radio"
                name="pause_mode"
                value="indefinite"
                checked={pauseMode === "indefinite"}
                onChange={() => setPauseMode("indefinite")}
              />
              {t("pauseIndefinite")}
            </label>
          </ActionForm>
        </Modal>
      )}
      {selected && (
        <WorkDetails
          entry={selected}
          activities={activities}
          onClose={() => setSelected(undefined)}
          onSaved={() => void synchronize()}
        />
      )}
    </>
  );
}

export function History({
  activities,
  userID,
}: {
  activities: Entity[];
  userID: string;
}) {
  const { t, date, hours } = useI18n();
  const [from, setFrom] = useState("");
  const [to, setTo] = useState("");
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<Work>();
  const result = useData<List<Work>>(
    `/work-sessions?from=${from}&to=${to}&page=${page}&page_size=25&worker_id=${userID}`,
  );
  return (
    <>
      <PageHeader title={t("history")} />
      <div className="filters">
        <Field label={t("from")}>
          <input
            type="date"
            value={from}
            onChange={(e) => {
              setFrom(e.target.value);
              setPage(1);
            }}
          />
        </Field>
        <Field label={t("to")}>
          <input
            type="date"
            value={to}
            onChange={(e) => {
              setTo(e.target.value);
              setPage(1);
            }}
          />
        </Field>
      </div>
      {result.error !== undefined && (
        <ErrorNotice error={result.error} retry={() => void result.refresh()} />
      )}{" "}
      {result.loading ? (
        <Loading />
      ) : result.data?.items.length ? (
        <>
          <div className="history-list">
            {result.data.items.map((entry) => (
              <button
                className="history-row"
                onClick={() => setSelected(entry)}
                key={entry.id}
              >
                <div className="date-block">
                  <span>{date(entry.started_at)}</span>
                  <strong>{entry.farm_name}</strong>
                  <small>{entry.activity_name || t("complete")}</small>
                </div>
                <div>
                  <strong>{hours(entry.net_us || entry.elapsed_us)}</strong>
                  <Status state={entry.state} />
                </div>
                <ArrowRight size={18} />
              </button>
            ))}
          </div>
          <div className="pager">
            <button
              className="button secondary"
              disabled={page <= 1}
              onClick={() => setPage(page - 1)}
            >
              {t("previous")}
            </button>
            <span>
              {t("page")} {page}
            </span>
            <button
              className="button secondary"
              disabled={page * 25 >= result.data.total}
              onClick={() => setPage(page + 1)}
            >
              {t("next")}
            </button>
          </div>
        </>
      ) : (
        <Empty text={t("noEntries")} />
      )}{" "}
      {selected && (
        <WorkDetails
          entry={selected}
          activities={activities}
          onClose={() => setSelected(undefined)}
          onSaved={() => void result.refresh()}
        />
      )}
    </>
  );
}
