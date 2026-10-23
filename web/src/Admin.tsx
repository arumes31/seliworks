import { useEffect, useState } from "react";

import { ArrowRight, Clock3, Plus, Square } from "lucide-react";

import { api, decimalMoney, errorText, formInstant } from "./api";

import type { Entity, List, Organization, Work } from "./api";

import { useData } from "./hooks";

import { stateName, useI18n } from "./i18n";

import {
  ActionForm,
  Empty,
  ErrorNotice,
  Field,
  Loading,
  Modal,
  PageHeader,
  Pager,
  Status,
} from "./ui";

import { WorkDetails } from "./WorkDetails";
import { LocalTimeField } from "./LocalTimeField";

interface OverviewData {
  today_us: number;
  month_us: number;
  year_us: number;
  pending_details: number;
  pending_approvals: number;
  unbilled_us: number;
  outstanding: string | null;
  outstanding_by_currency: { currency: string; outstanding: string }[];
  running: Work[];
  daily: { date: string; net_us: number }[];
  farms: { name: string; net_us: number }[];
}

export function Overview({
  onEntries,
  organization,
}: {
  onEntries: () => void;
  organization: Organization;
}) {
  const { t, hours, lang, date } = useI18n();
  const result = useData<OverviewData>("/admin/overview");
  const [stopping, setStopping] = useState<Work>();
  const [stopKey, setStopKey] = useState("");
  useEffect(() => {
    const refresh = () => {
      if (!document.hidden) void result.refresh();
    };
    const interval = setInterval(refresh, 30000);
    window.addEventListener("focus", refresh);
    return () => {
      clearInterval(interval);
      window.removeEventListener("focus", refresh);
    };
  }, [result.refresh]);
  if (result.loading) return <Loading />;
  return (
    <>
      <PageHeader title={t("overview")} description={organization.name} />
      {result.error !== undefined && (
        <ErrorNotice error={result.error} retry={() => void result.refresh()} />
      )}{" "}
      {result.data && (
        <>
          <section className="overview-summary">
            {(["today", "month", "year"] as const).map((key) => (
              <div key={key}>
                <span>{t(key)}</span>
                <strong>{hours(result.data![`${key}_us`])}</strong>
              </div>
            ))}
          </section>
          <p className="muted small">{t("provisional")}</p>
          <div className="admin-columns">
            <section className="panel running-panel">
              <div className="section-title">
                <h2>
                  <Clock3 size={21} />
                  {t("runningSessions")}
                </h2>
                <span className="count">{result.data.running.length}</span>
              </div>
              {result.data.running.length ? (
                result.data.running.map((entry) => (
                  <div className="running-row" key={entry.id}>
                    <div className="avatar">
                      {entry.worker_name?.slice(0, 2).toUpperCase()}
                    </div>
                    <div>
                      <strong>{entry.worker_name}</strong>
                      <span>{entry.farm_name}</span>
                      <small>{date(entry.started_at)}</small>
                      {entry.elapsed_us >
                        organization.long_session_hours * 3600e6 && (
                        <span className="warning-text">{t("longHours")}</span>
                      )}
                    </div>
                    <strong>{hours(entry.elapsed_us)}</strong>
                    <button
                      className="icon-button danger-text"
                      aria-label={t("stopOther")}
                      onClick={() => {
                        setStopping(entry);
                        setStopKey(crypto.randomUUID());
                      }}
                    >
                      <Square size={17} />
                    </button>
                  </div>
                ))
              ) : (
                <Empty text={t("noRunning")} />
              )}
            </section>
            <section className="panel attention-panel">
              <h2>{t("review")}</h2>
              <button onClick={onEntries}>
                <span>{t("pendingApprovals")}</span>
                <strong>{result.data.pending_approvals}</strong>
                <ArrowRight size={18} />
              </button>
              <button onClick={onEntries}>
                <span>{t("pendingDetails")}</span>
                <strong>{result.data.pending_details}</strong>
                <ArrowRight size={18} />
              </button>
              <div>
                <span>{t("unbilled")}</span>
                <strong>{hours(result.data.unbilled_us)}</strong>
              </div>
              <div>
                <span>{t("outstanding")}</span>
                <strong>
                  {result.data.outstanding_by_currency.map((row) => (
                    <span className="block" key={row.currency}>
                      {decimalMoney(
                        row.outstanding,
                        row.currency,
                        lang === "de" ? "de-AT" : "en-GB",
                      )}
                    </span>
                  ))}
                </strong>
              </div>
            </section>
          </div>
          <section className="panel section-block">
            <h2>{t("farms")}</h2>
            {result.data.farms.length ? (
              <div
                className="table-wrap"
                role="region"
                aria-label={t("farms")}
                tabIndex={0}
              >
                <table>
                  <thead>
                    <tr>
                      <th>{t("farm")}</th>
                      <th>{t("duration")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {result.data.farms.map((farm, i) => (
                      <tr key={i}>
                        <td>{farm.name}</td>
                        <td>{hours(farm.net_us)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <Empty />
            )}
          </section>
        </>
      )}
      {stopping && (
        <Modal title={t("stopOther")} onClose={() => setStopping(undefined)}>
          <p>{t("stopReason")}</p>
          <ActionForm
            label={t("stopOther")}
            onSubmit={async (form) => {
              await api(
                `/work-sessions/${stopping.id}/stop`,
                "POST",
                { reason: form.get("reason") },
                stopKey,
              );
              setStopping(undefined);
              await result.refresh();
            }}
          >
            <Field label={t("reason")}>
              <textarea required name="reason" maxLength={2000} />
            </Field>
          </ActionForm>
        </Modal>
      )}
    </>
  );
}

export function Entries({
  farms,
  activities,
  workers,
}: {
  farms: Entity[];
  activities: Entity[];
  workers: Entity[];
}) {
  const { t, hours, date, lang } = useI18n();
  const [filters, setFilters] = useState({
    search: "",
    state: "",
    farm_id: "",
    worker_id: "",
    from: "",
    to: "",
  });
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<Work>();
  const [manual, setManual] = useState(false);
  const [corrections, setCorrections] = useState(false);
  const [sort, setSort] = useState("newest");
  const [billing, setBilling] = useState("");
  const [checked, setChecked] = useState<Work[]>([]);
  const [bulkOpen, setBulkOpen] = useState(false);
  const [bulkOutcome, setBulkOutcome] = useState("");
  const result = useData<List<Work>>(
    `/work-sessions?${new URLSearchParams({ ...filters, sort, billing, page: String(page), page_size: "25" })}`,
  );
  useEffect(() => {
    setChecked([]);
  }, [page, filters, sort, billing]);
  const set = (name: keyof typeof filters, value: string) => {
    setFilters({ ...filters, [name]: value });
    setPage(1);
  };
  const entries = result.data?.items ?? [];
  return (
    <>
      <PageHeader
        title={t("entries")}
        action={
          <div className="button-group">
            {checked.length > 0 && (
              <button
                className="button secondary"
                onClick={() => {
                  setBulkOpen(true);
                  setBulkOutcome("");
                }}
              >
                {t("approveSelected")} ({checked.length})
              </button>
            )}
            <button
              className="button secondary"
              onClick={() => setCorrections(true)}
            >
              {t("corrections")}
            </button>
            <button className="button primary" onClick={() => setManual(true)}>
              <Plus size={18} />
              {t("manualEntry")}
            </button>
          </div>
        }
      />
      <div className="filters entry-filters">
        <Field label={t("search")}>
          <input
            type="search"
            value={filters.search}
            onChange={(e) => set("search", e.target.value)}
          />
        </Field>
        <Field label={t("status")}>
          <select
            value={filters.state}
            onChange={(e) => set("state", e.target.value)}
          >
            <option value="">{t("all")}</option>
            {[
              "RUNNING",
              "STOPPED_PENDING_DETAILS",
              "SUBMITTED",
              "REJECTED",
              "APPROVED",
            ].map((s) => (
              <option key={s} value={s}>
                {stateName(s, lang)}
              </option>
            ))}
          </select>
        </Field>
        {(["farm_id", "worker_id"] as const).map((key) => (
          <Field key={key} label={t(key === "farm_id" ? "farm" : "worker")}>
            <select
              value={filters[key]}
              onChange={(e) => set(key, e.target.value)}
            >
              <option value="">{t("all")}</option>
              {(key === "farm_id" ? farms : workers).map((e) => (
                <option key={e.id} value={e.id}>
                  {e.name}
                </option>
              ))}
            </select>
          </Field>
        ))}
        <Field label={t("from")}>
          <input
            type="date"
            value={filters.from}
            onChange={(e) => set("from", e.target.value)}
          />
        </Field>
        <Field label={t("to")}>
          <input
            type="date"
            value={filters.to}
            onChange={(e) => set("to", e.target.value)}
          />
        </Field>
        <Field label={t("sort")}>
          <select
            value={sort}
            onChange={(e) => {
              setSort(e.target.value);
              setPage(1);
            }}
          >
            <option value="newest">{t("newest")}</option>
            <option value="oldest">{t("oldest")}</option>
            <option value="worker">{t("byWorker")}</option>
          </select>
        </Field>
        <Field label={t("billing")}>
          <select
            value={billing}
            onChange={(e) => {
              setBilling(e.target.value);
              setPage(1);
            }}
          >
            <option value="">{t("allBilling")}</option>
            <option value="billable">{t("billable")}</option>
            <option value="nonbillable">{t("nonBillable")}</option>
            <option value="unbilled">{t("unbilled")}</option>
            <option value="reserved">{t("reserved")}</option>
            <option value="issued">{t("invoiced")}</option>
          </select>
        </Field>
      </div>
      {result.error !== undefined && (
        <ErrorNotice error={result.error} retry={() => void result.refresh()} />
      )}{" "}
      {result.loading ? (
        <Loading />
      ) : entries.length ? (
        <div className="panel table-panel">
          <div
            className="table-wrap"
            role="region"
            aria-label={t("entries")}
            tabIndex={0}
          >
            <table>
              <thead>
                <tr>
                  <th>{t("select")}</th>
                  <th>{t("worker")}</th>
                  <th>{t("farm")}</th>
                  <th>{t("activity")}</th>
                  <th>{t("date")}</th>
                  <th>{t("duration")}</th>
                  <th>{t("status")}</th>
                  <th scope="col">{t("actions")}</th>
                </tr>
              </thead>
              <tbody>
                {entries.map((entry) => (
                  <tr key={entry.id}>
                    <td>
                      {entry.state === "SUBMITTED" && (
                        <input
                          type="checkbox"
                          aria-label={`${t("select")} ${entry.worker_name} ${date(entry.started_at)}`}
                          checked={checked.some((item) => item.id === entry.id)}
                          onChange={(e) =>
                            setChecked(
                              e.target.checked
                                ? [...checked, entry]
                                : checked.filter(
                                    (item) => item.id !== entry.id,
                                  ),
                            )
                          }
                        />
                      )}
                    </td>
                    <td>
                      <strong>{entry.worker_name}</strong>
                    </td>
                    <td>{entry.farm_name}</td>
                    <td>{entry.activity_name || "—"}</td>
                    <td>{date(entry.started_at)}</td>
                    <td className="numeric">
                      {hours(entry.net_us || entry.elapsed_us)}
                    </td>
                    <td>
                      <Status state={entry.state} />
                      {entry.pricing_status === "RATE_REQUIRED" && (
                        <small className="block">{t("rateRequired")}</small>
                      )}
                      {entry.pricing_status?.endsWith("_POLICY_REQUIRED") && (
                        <small className="block">
                          {t("surchargeRequired")}
                        </small>
                      )}
                    </td>
                    <td>
                      <button
                        className="icon-button"
                        aria-label={t("details")}
                        onClick={() => setSelected(entry)}
                      >
                        <ArrowRight size={18} />
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Pager
            page={page}
            total={result.data?.total ?? 0}
            size={25}
            onChange={setPage}
          />
        </div>
      ) : (
        <Empty />
      )}
      {bulkOpen && (
        <Modal title={t("approveSelected")} onClose={() => setBulkOpen(false)}>
          <p>{t("bulkHint")}</p>
          {checked.map((entry) => (
            <p key={entry.id}>
              {entry.worker_name} · {entry.farm_name} · {hours(entry.net_us)}
            </p>
          ))}
          {bulkOutcome && (
            <div role="status" className="notice">
              {bulkOutcome}
            </div>
          )}
          <ActionForm
            label={t("approve")}
            onSubmit={async () => {
              let completed = 0;
              let failure = "";
              for (const entry of checked) {
                try {
                  await api(
                    `/admin/work-sessions/${entry.id}/approve`,
                    "POST",
                    { version: entry.version },
                  );
                  completed++;
                } catch (error) {
                  failure = errorText(error, lang);
                  break;
                }
              }
              setBulkOutcome(
                `${completed} / ${checked.length} ${t("approvedCount")}${failure ? `. ${failure}` : ""}`,
              );
              setChecked([]);
              await result.refresh();
            }}
          >
            <></>
          </ActionForm>
        </Modal>
      )}
      {selected && (
        <WorkDetails
          farms={farms}
          workers={workers}
          entry={selected}
          activities={activities}
          admin
          onClose={() => setSelected(undefined)}
          onSaved={() => void result.refresh()}
        />
      )}{" "}
      {manual && (
        <Modal title={t("manualEntry")} onClose={() => setManual(false)}>
          <ActionForm
            label={t("create")}
            onSubmit={async (form) => {
              await api("/admin/work-sessions/manual", "POST", {
                worker_id: form.get("worker_id"),
                farm_id: form.get("farm_id"),
                activity_id: form.get("activity_id"),
                started_at: formInstant(form, "started_at"),
                stopped_at: formInstant(form, "stopped_at"),
                description: form.get("description"),
                reason: form.get("reason"),
              });
              setManual(false);
              await result.refresh();
            }}
          >
            {(
              [
                ["worker_id", "worker", workers],
                ["farm_id", "farm", farms],
                ["activity_id", "activity", activities],
              ] as const
            ).map(([name, label, items]) => (
              <Field label={t(label)} key={name}>
                <select required name={name}>
                  <option value="">{t("select")}</option>
                  {items
                    .filter((e) => e.active)
                    .map((e) => (
                      <option key={e.id} value={e.id}>
                        {e.name}
                      </option>
                    ))}
                </select>
              </Field>
            ))}
            <LocalTimeField label={t("startTime")} name="started_at" />
            <LocalTimeField label={t("stopTime")} name="stopped_at" />
            <Field label={t("description")}>
              <textarea required name="description" maxLength={4000} />
            </Field>
            <Field label={t("reason")}>
              <textarea required name="reason" maxLength={2000} />
            </Field>
            <p className="muted small">{t("localTimezone")}</p>
          </ActionForm>
        </Modal>
      )}
      {corrections && (
        <Corrections
          onClose={() => setCorrections(false)}
          onReviewed={() => void result.refresh()}
        />
      )}
    </>
  );
}

interface Correction {
  id: string;
  work_session_id: string;
  worker_name: string;
  requested_start: string;
  requested_stop: string;
  reason: string;
  state: string;
}

function Corrections({
  onClose,
  onReviewed,
}: {
  onClose: () => void;
  onReviewed: () => void;
}) {
  const { t, date } = useI18n();
  const result = useData<List<Correction>>("/admin/corrections");
  return (
    <Modal title={t("reviewCorrections")} onClose={onClose} wide>
      {result.error !== undefined && <ErrorNotice error={result.error} />}{" "}
      {result.loading ? (
        <Loading />
      ) : result.data?.items.length ? (
        result.data.items.map((c) => (
          <section className="correction" key={c.id}>
            <strong>{c.worker_name || c.work_session_id}</strong>
            <p>
              {date(c.requested_start)} — {date(c.requested_stop)}
            </p>
            <p>{c.reason}</p>
            <Status state={c.state} />
            {["PENDING", "pending"].includes(c.state) && (
              <ActionForm
                label={t("review")}
                onSubmit={async (form) => {
                  await api(`/admin/corrections/${c.id}/review`, "POST", {
                    approve: form.get("approve") === "true",
                    reason: form.get("reason"),
                  });
                  await result.refresh();
                  onReviewed();
                }}
              >
                <Field label={t("review")}>
                  <select name="approve">
                    <option value="true">{t("approveCorrection")}</option>
                    <option value="false">{t("rejectCorrection")}</option>
                  </select>
                </Field>
                <Field label={t("reason")}>
                  <textarea name="reason" required maxLength={2000} />
                </Field>
              </ActionForm>
            )}
          </section>
        ))
      ) : (
        <Empty />
      )}
    </Modal>
  );
}

export function Management({
  kind,
  farms,
  onChanged,
}: {
  kind: "workers" | "farms" | "activities";
  farms: Entity[];
  onChanged: () => void;
}) {
  const { t } = useI18n();
  const result = useData<List<Entity>>(`/admin/${kind}`);
  const [editing, setEditing] = useState<Entity | null | undefined>();
  const [assigning, setAssigning] = useState<Entity>();
  const [inviting, setInviting] = useState<Entity>();
  const [setupLink, setSetupLink] = useState<string>();
  return (
    <>
      <PageHeader
        title={t(kind)}
        action={
          <button className="button primary" onClick={() => setEditing(null)}>
            <Plus size={18} />
            {t("create")}
          </button>
        }
      />
      {result.error !== undefined && (
        <ErrorNotice error={result.error} retry={() => void result.refresh()} />
      )}{" "}
      {result.loading ? (
        <Loading />
      ) : result.data?.items.length ? (
        <div className="panel table-panel">
          <div
            className="table-wrap"
            role="region"
            aria-label={t(kind)}
            tabIndex={0}
          >
            <table>
              <thead>
                <tr>
                  <th>{t("name")}</th>
                  {kind !== "activities" && <th>{t("email")}</th>}
                  <th>{t("status")}</th>
                  <th scope="col">{t("actions")}</th>
                </tr>
              </thead>
              <tbody>
                {result.data.items.map((entity) => (
                  <tr key={entity.id}>
                    <td>
                      <strong>{entity.name}</strong>
                      {entity.is_admin && (
                        <small className="block muted">
                          {t("administrator")}
                        </small>
                      )}
                    </td>
                    {kind !== "activities" && <td>{entity.email || "—"}</td>}
                    <td>
                      <span
                        className={`status ${entity.active ? "" : "archived"}`}
                      >
                        {t(entity.active ? "active" : "inactive")}
                      </span>
                      {kind === "workers" && entity.running && (
                        <p className="small">
                          {t("runningAccountWarning")}{" "}
                          <a href="#overview">{t("overview")}</a>
                        </p>
                      )}
                    </td>
                    <td>
                      <div className="button-group">
                        {kind === "workers" && (
                          <button
                            className="text-button"
                            onClick={() => {
                              setInviting(entity);
                              setSetupLink(undefined);
                            }}
                          >
                            {t("invite")}
                          </button>
                        )}
                        {kind === "workers" && (
                          <button
                            className="text-button"
                            onClick={() => setAssigning(entity)}
                          >
                            {t("assignFarms")}
                          </button>
                        )}
                        <button
                          className="button secondary small-button"
                          onClick={() => setEditing(entity)}
                        >
                          {t("edit")}
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      ) : (
        <Empty />
      )}
      {editing !== undefined && (
        <Modal
          title={`${t(editing ? "edit" : "create")} · ${t(kind)}`}
          onClose={() => setEditing(undefined)}
        >
          {kind === "workers" && editing?.running && (
            <p className="notice warning">{t("runningAccountWarning")}</p>
          )}
          <ActionForm
            label={t("save")}
            onSubmit={async (form) => {
              const body: Record<string, unknown> = { name: form.get("name") };
              if (kind !== "activities") {
                body.email = form.get("email");
                if (kind === "farms") body.address = form.get("address");
              }
              if (editing) {
                body.version = editing.version;
                const active = form.get("active") === "on";
                if (kind !== "workers" || active !== editing.active)
                  body.active = active;
              }
              if (kind === "workers") {
                body.is_admin = form.get("is_admin") === "on";
                body.is_worker = form.get("is_worker") === "on";
                if (!editing && form.get("password"))
                  body.password = form.get("password");
              }
              await api(
                `/admin/${kind}${editing ? "/" + editing.id : ""}`,
                editing ? "PATCH" : "POST",
                body,
              );
              setEditing(undefined);
              await result.refresh();
              onChanged();
            }}
          >
            <Field label={t("name")}>
              <input
                name="name"
                required
                maxLength={200}
                defaultValue={editing?.name}
              />
            </Field>
            {kind !== "activities" && (
              <>
                <Field label={t("email")}>
                  <input
                    name="email"
                    type="email"
                    required={kind === "workers"}
                    defaultValue={editing?.email}
                    maxLength={254}
                  />
                </Field>
                {kind === "farms" && (
                  <Field label={t("address")}>
                    <textarea
                      name="address"
                      defaultValue={editing?.address}
                      maxLength={4000}
                    />
                  </Field>
                )}
              </>
            )}
            {kind === "workers" && (
              <>
                {!editing && (
                  <Field label={t("newPassword")}>
                    <input
                      name="password"
                      type="password"
                      autoComplete="new-password"
                      minLength={12}
                      maxLength={256}
                    />
                  </Field>
                )}
                {!editing && (
                  <p className="muted small">{t("optionalPassword")}</p>
                )}
                <label className="check">
                  <input
                    name="is_admin"
                    type="checkbox"
                    defaultChecked={editing?.is_admin}
                  />
                  {t("administrator")}
                </label>
                <label className="check">
                  <input
                    name="is_worker"
                    type="checkbox"
                    defaultChecked={editing?.is_worker ?? true}
                  />
                  {t("workerPermission")}
                </label>
              </>
            )}
            {editing && (
              <label className="check">
                <input
                  type="checkbox"
                  name="active"
                  defaultChecked={editing.active}
                />
                {t("active")}
              </label>
            )}
          </ActionForm>
        </Modal>
      )}
      {inviting && (
        <Modal
          title={`${t("invite")} · ${inviting.name}`}
          onClose={() => {
            setInviting(undefined);
            setSetupLink(undefined);
          }}
        >
          <p>{t("inviteHint")}</p>
          {setupLink ? (
            <Field label={t("invite")}>
              <input
                readOnly
                value={setupLink}
                onFocus={(e) => e.target.select()}
                autoComplete="off"
              />
            </Field>
          ) : (
            <ActionForm
              label={t("invite")}
              onSubmit={async () => {
                const result = await api<{ setup_url: string }>(
                  `/admin/workers/${inviting.id}/invite`,
                  "POST",
                  {},
                );
                setSetupLink(result.setup_url);
              }}
            >
              <></>
            </ActionForm>
          )}
        </Modal>
      )}
      {assigning && (
        <Modal
          title={`${t("assignFarms")} · ${assigning.name}`}
          onClose={() => setAssigning(undefined)}
        >
          <ActionForm
            label={t("save")}
            onSubmit={async (form) => {
              await api(`/admin/workers/${assigning.id}/farms`, "PUT", {
                farm_ids: form.getAll("farm_ids"),
              });
              setAssigning(undefined);
              onChanged();
            }}
          >
            {farms.map((farm) => (
              <label key={farm.id} className="check">
                <input
                  type="checkbox"
                  name="farm_ids"
                  value={farm.id}
                  defaultChecked={assigning.farm_ids?.includes(farm.id)}
                />
                {farm.name}
              </label>
            ))}
          </ActionForm>
        </Modal>
      )}
    </>
  );
}
