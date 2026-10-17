import { useRef, useState } from "react";
import { ArrowRight, Download, Plus, Printer } from "lucide-react";
import {
  api,
  calendarDate,
  decimalInput,
  decimalMoney,
  formInstant,
} from "./api";
import type { Entity, List } from "./api";
import { useData } from "./hooks";
import { stateName, useI18n } from "./i18n";
import { SpecialDayPricing } from "./SpecialDayPricing";
import { InvoiceSummary } from "./InvoiceSummary";
import type { InvoiceSummaryLine } from "./InvoiceSummary";
import { InvoiceDelivery } from "./InvoiceDelivery";
import { InvoiceHistory } from "./InvoiceHistory";
import { LocalTimeField } from "./LocalTimeField";
import type { InvoicePayment, InvoiceCredit } from "./InvoiceHistory";
import { reportPeriod } from "./report-periods";
import type { ReportPreset } from "./report-periods";
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
interface Rate {
  id: string;
  worker_id: string | null;
  farm_id: string | null;
  activity_id: string | null;
  hourly_rate: string;
  effective_from: string;
  effective_to: string | null;
  version: number;
}
export function Rates({
  workers,
  farms,
  activities,
}: {
  workers: Entity[];
  farms: Entity[];
  activities: Entity[];
}) {
  const { t, money, date, lang } = useI18n();
  const result = useData<List<Rate>>("/admin/rates");
  const [editing, setEditing] = useState<Rate | null | undefined>();
  return (
    <>
      <PageHeader
        title={t("rates")}
        description={t("rateHint")}
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
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>{t("worker")}</th>
                  <th>{t("farm")}</th>
                  <th>{t("activity")}</th>
                  <th>{t("hourlyRate")}</th>
                  <th>{t("effectiveFrom")}</th>
                  <th>{t("effectiveTo")}</th>
                  <th scope="col">{t("actions")}</th>
                </tr>
              </thead>
              <tbody>
                {result.data.items.map((rate) => (
                  <tr key={rate.id}>
                    <td>
                      {workers.find((w) => w.id === rate.worker_id)?.name ||
                        t("all")}
                    </td>
                    <td>
                      {farms.find((f) => f.id === rate.farm_id)?.name ||
                        t("all")}
                    </td>
                    <td>
                      {activities.find((a) => a.id === rate.activity_id)
                        ?.name || t("all")}
                    </td>
                    <td className="numeric">
                      <strong>{money(rate.hourly_rate)}</strong>
                    </td>
                    <td>{date(rate.effective_from)}</td>
                    <td>{rate.effective_to ? date(rate.effective_to) : "—"}</td>
                    <td>
                      <button
                        className="text-button"
                        onClick={() => setEditing(rate)}
                      >
                        {t("edit")}
                      </button>
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
      <SpecialDayPricing />
      {editing !== undefined && (
        <Modal
          title={`${t(editing ? "edit" : "create")} · ${t("hourlyRate")}`}
          onClose={() => setEditing(undefined)}
        >
          <ActionForm
            label={t("save")}
            onSubmit={async (form) => {
              await api(
                `/admin/rates${editing ? "/" + editing.id : ""}`,
                editing ? "PATCH" : "POST",
                {
                  version: editing?.version,
                  worker_id: form.get("worker_id") || null,
                  farm_id: form.get("farm_id") || null,
                  activity_id: form.get("activity_id") || null,
                  hourly_rate: decimalInput(
                    String(form.get("hourly_rate") || ""),
                    lang,
                  ),
                  effective_from: formInstant(
                    form,
                    "effective_from",
                    editing?.effective_from,
                  ),
                  effective_to: form.get("effective_to")
                    ? formInstant(
                        form,
                        "effective_to",
                        editing?.effective_to || undefined,
                      )
                    : null,
                },
              );
              setEditing(undefined);
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
                <select name={name} defaultValue={editing?.[name] || ""}>
                  <option value="">{t("all")}</option>
                  {items.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.name}
                    </option>
                  ))}
                </select>
              </Field>
            ))}
            <Field label={t("hourlyRate")} hint={t("decimalHint")}>
              <input
                required
                name="hourly_rate"
                inputMode="decimal"
                pattern={
                  lang === "de"
                    ? "[0-9]+([.,][0-9]{1,4})?"
                    : "[0-9]+([.][0-9]{1,4})?"
                }
                defaultValue={
                  lang === "de"
                    ? editing?.hourly_rate.replace(".", ",")
                    : editing?.hourly_rate
                }
                placeholder={lang === "de" ? "0,00" : "0.00"}
              />
            </Field>
            <LocalTimeField
              label={t("effectiveFrom")}
              name="effective_from"
              instant={editing?.effective_from}
            />
            <LocalTimeField
              label={t("effectiveTo")}
              name="effective_to"
              instant={editing?.effective_to || undefined}
              required={false}
            />
            <small className="muted">{t("localTimezone")}</small>
          </ActionForm>
        </Modal>
      )}
    </>
  );
}
interface ReportRow {
  additional_deduction_us: number;
  unbilled_us?: number;
  label: string;
  gross_us: number;
  break_us: number;
  net_us: number;
  approved_us: number;
}
interface Report {
  finance?: {
    unbilled_us: number;
    by_currency: {
      currency: string;
      invoiced_net: string;
      paid: string;
      balance: string;
    }[];
  };
  rows: ReportRow[];
  totals: Omit<ReportRow, "label">;
}
function defaultDates(timezone: string) {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: timezone,
    year: "numeric",
    month: "2-digit",
  }).formatToParts(new Date());
  const year = Number(parts.find((p) => p.type === "year")!.value);
  const month = Number(parts.find((p) => p.type === "month")!.value);
  const start = `${year}-${String(month).padStart(2, "0")}-01`;
  const n = new Date(Date.UTC(year, month, 1));
  return {
    from: start,
    to: `${n.getUTCFullYear()}-${String(n.getUTCMonth() + 1).padStart(2, "0")}-01`,
  };
}
export function Reports() {
  const { t, hours, lang, timezone } = useI18n();
  const [period, setPeriod] = useState(() => defaultDates(timezone));
  const [group, setGroup] = useState("worker");
  const query = new URLSearchParams({ ...period, group });
  const result = useData<Report>("/admin/reports?" + query);
  return (
    <>
      <PageHeader
        title={t("reports")}
        action={
          <div className="button-group">
            <a
              className="button secondary"
              href={"/api/v1/admin/reports.csv?" + query}
            >
              <Download size={18} />
              {t("export")}
            </a>
            <button className="button secondary" onClick={() => window.print()}>
              <Printer size={18} />
              {t("print")}
            </button>
          </div>
        }
      />
      <div className="filters">
        <Field label={t("reportPeriod")}>
          <select
            aria-label={t("reportPeriod")}
            defaultValue=""
            onChange={(event) => {
              const preset = event.target.value as ReportPreset | "";
              if (preset) setPeriod(reportPeriod(preset, timezone));
              event.target.value = "";
            }}
          >
            <option value="">{t("customPeriod")}</option>
            <option value="day">{t("today")}</option>
            <option value="week">{t("thisWeek")}</option>
            <option value="month">{t("month")}</option>
            <option value="year">{t("year")}</option>
          </select>
        </Field>
        <Field label={t("from")}>
          <input
            type="date"
            value={period.from}
            onChange={(e) => setPeriod({ ...period, from: e.target.value })}
          />
        </Field>
        <Field label={t("to")}>
          <input
            type="date"
            value={period.to}
            onChange={(e) => setPeriod({ ...period, to: e.target.value })}
          />
        </Field>
        <Field label={t("group")}>
          <select value={group} onChange={(e) => setGroup(e.target.value)}>
            {(["worker", "farm", "activity", "day"] as const).map((g) => (
              <option value={g} key={g}>
                {t(g)}
              </option>
            ))}
          </select>
        </Field>
      </div>
      {result.error !== undefined && (
        <ErrorNotice error={result.error} retry={() => void result.refresh()} />
      )}{" "}
      {result.loading ? (
        <Loading />
      ) : result.data?.rows.length ? (
        <>
          <section
            className="report-chart panel"
            aria-label={t("reports")}
            tabIndex={0}
          >
            {result.data.rows.map((row, i) => (
              <div className="chart-row" key={i}>
                <span>{row.label}</span>
                <meter
                  min={0}
                  max={Math.max(...result.data!.rows.map((r) => r.net_us), 1)}
                  value={row.net_us}
                >
                  {hours(row.net_us)}
                </meter>
                <strong>{hours(row.net_us)}</strong>
              </div>
            ))}
          </section>
          <div className="panel table-panel section-block">
            <div
              className="table-wrap"
              role="region"
              aria-label={`${t("reports")} – ${t("total")}`}
              tabIndex={0}
            >
              <table>
                <thead>
                  <tr>
                    <th>
                      {t(group as "worker" | "farm" | "activity" | "day")}
                    </th>
                    <th>{t("gross")}</th>
                    <th>{t("breaks")}</th>
                    <th>{t("deduction")}</th>
                    <th>{t("duration")}</th>
                    <th>{t("approved")}</th>
                    <th>{t("unbilled")}</th>
                  </tr>
                </thead>
                <tbody>
                  {result.data.rows.map((row, i) => (
                    <tr key={i}>
                      <td>{row.label}</td>
                      <td>{hours(row.gross_us)}</td>
                      <td>{hours(row.break_us)}</td>
                      <td>{hours(row.additional_deduction_us)}</td>
                      <td>{hours(row.net_us)}</td>
                      <td>{hours(row.approved_us)}</td>
                      <td>{hours(row.unbilled_us ?? 0)}</td>
                    </tr>
                  ))}
                </tbody>
                <tfoot>
                  <tr>
                    <th>{t("total")}</th>
                    <td>{hours(result.data.totals.gross_us)}</td>
                    <td>{hours(result.data.totals.break_us)}</td>
                    <td>{hours(result.data.totals.additional_deduction_us)}</td>
                    <td>{hours(result.data.totals.net_us)}</td>
                    <td>{hours(result.data.totals.approved_us)}</td>
                    <td>{hours(result.data.totals.unbilled_us ?? 0)}</td>
                  </tr>
                </tfoot>
              </table>
            </div>
          </div>
          {result.data.finance && (
            <section className="panel section-block">
              <h2>{t("billing")}</h2>
              <p className="muted small">{t("invoiceBalanceScope")}</p>
              {result.data.finance.by_currency.map((row) => (
                <dl className="report-finance" key={row.currency}>
                  {(
                    [
                      ["invoiced_net", "invoicedNet"],
                      ["paid", "paid"],
                      ["balance", "balance"],
                    ] as const
                  ).map(([key, label]) => (
                    <div key={key}>
                      <dt>{t(label)}</dt>
                      <dd>
                        {decimalMoney(
                          row[key],
                          row.currency,
                          lang === "de" ? "de-AT" : "en-GB",
                        )}
                      </dd>
                    </div>
                  ))}
                </dl>
              ))}
            </section>
          )}
        </>
      ) : (
        <Empty />
      )}
    </>
  );
}
interface InvoiceItem {
  pricing?: {
    day_type?: "REGULAR" | "VACATION" | "HOLIDAY";
    special_day_policy?: {
      day_type: "VACATION" | "HOLIDAY";
      mode: "percentage" | "hourly";
      value: string;
      version: number;
    } | null;
    segments: {
      hourly_rate: string;
      base_hourly_rate?: string;
      rule_id: string;
      rule_version: number;
    }[];
  };
  id?: string;
  description: string;
  quantity?: string;
  hourly_rate?: string;
  rate?: string;
  net?: string;
  amount?: string;
  duration_us?: number;
}
interface Invoice {
  id: string;
  payment_state: string;
  overdue: boolean;
  refund_due?: string;
  payments?: InvoicePayment[];
  credits?: InvoiceCredit[];
  issuer?: {
    tax_identifier: string;
    invoice_footer: string;
    invoice_layout: string;
    invoice_appendix: boolean;
  };
  summary_lines?: InvoiceSummaryLine[];
  number: string | null;
  state: string;
  farm_name: string;
  from_date: string;
  to_date: string;
  net: string;
  tax: string;
  total: string;
  paid: string;
  balance: string;
  currency: string;
  items: InvoiceItem[];
  due_on?: string | null;
  issued_at?: string;
}
export function Invoices({
  farms,
  smtpAvailable = false,
}: {
  farms: Entity[];
  smtpAvailable?: boolean;
}) {
  const { t, lang, timezone } = useI18n();
  const result = useData<List<Invoice>>("/admin/invoices");
  const [creating, setCreating] = useState(false);
  const [selected, setSelected] = useState<string>();
  const [draftKey, setDraftKey] = useState("");
  const period = defaultDates(timezone);
  return (
    <>
      <PageHeader
        title={t("invoices")}
        action={
          <button
            className="button primary"
            onClick={() => {
              setCreating(true);
              setDraftKey(crypto.randomUUID());
            }}
          >
            <Plus size={18} />
            {t("createDraft")}
          </button>
        }
      />
      <p className="muted">{t("draftHint")}</p>
      {result.error !== undefined && (
        <ErrorNotice error={result.error} retry={() => void result.refresh()} />
      )}{" "}
      {result.loading ? (
        <Loading />
      ) : result.data?.items.length ? (
        <div className="panel table-panel">
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>{t("number")}</th>
                  <th>{t("farm")}</th>
                  <th>{t("servicePeriod")}</th>
                  <th>{t("status")}</th>
                  <th>{t("total")}</th>
                  <th>{t("balance")}</th>
                  <th scope="col">{t("actions")}</th>
                </tr>
              </thead>
              <tbody>
                {result.data.items.map((invoice) => (
                  <tr key={invoice.id}>
                    <td>
                      <strong>{invoice.number || "—"}</strong>
                    </td>
                    <td>{invoice.farm_name}</td>
                    <td>
                      {calendarDate(
                        invoice.from_date,
                        lang === "de" ? "de-AT" : "en-GB",
                      )}{" "}
                      —{" "}
                      {calendarDate(
                        invoice.to_date,
                        lang === "de" ? "de-AT" : "en-GB",
                      )}
                    </td>
                    <td>
                      <Status state={invoice.state} />
                      <small className="block">
                        {stateName(invoice.payment_state, lang)}
                        {invoice.overdue && ` · ${t("overdue")}`}
                      </small>
                    </td>
                    <td className="numeric">
                      {decimalMoney(
                        invoice.total,
                        invoice.currency,
                        lang === "de" ? "de-AT" : "en-GB",
                      )}
                    </td>
                    <td className="numeric">
                      {decimalMoney(
                        invoice.balance,
                        invoice.currency,
                        lang === "de" ? "de-AT" : "en-GB",
                      )}
                    </td>
                    <td>
                      <button
                        className="icon-button"
                        aria-label={t("invoiceDetails")}
                        onClick={() => setSelected(invoice.id)}
                      >
                        <ArrowRight size={18} />
                      </button>
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
      {creating && (
        <Modal title={t("createDraft")} onClose={() => setCreating(false)}>
          <p className="muted">{t("draftHint")}</p>
          <ActionForm
            label={t("createDraft")}
            onSubmit={async (form) => {
              const invoice = await api<Invoice>(
                "/admin/invoices",
                "POST",
                {
                  farm_id: form.get("farm_id"),
                  from: form.get("from"),
                  to: form.get("to"),
                },
                draftKey,
              );
              setCreating(false);
              setSelected(invoice.id);
              await result.refresh();
            }}
          >
            <Field label={t("farm")}>
              <select required name="farm_id">
                <option value="">{t("select")}</option>
                {farms
                  .filter((f) => f.active)
                  .map((f) => (
                    <option value={f.id} key={f.id}>
                      {f.name}
                    </option>
                  ))}
              </select>
            </Field>
            <Field label={t("from")}>
              <input
                name="from"
                type="date"
                required
                defaultValue={period.from}
              />
            </Field>
            <Field label={t("to")}>
              <input name="to" type="date" required defaultValue={period.to} />
            </Field>
          </ActionForm>
        </Modal>
      )}
      {selected && (
        <InvoiceDetails
          smtpAvailable={smtpAvailable}
          id={selected}
          onClose={() => setSelected(undefined)}
          onChange={() => void result.refresh()}
        />
      )}
    </>
  );
}
function InvoiceDetails({
  smtpAvailable,
  id,
  onClose,
  onChange,
}: {
  smtpAvailable: boolean;
  id: string;
  onClose: () => void;
  onChange: () => void;
}) {
  const { t, lang, currency } = useI18n();
  const result = useData<Invoice>(`/admin/invoices/${id}`);
  const money = (value: string) =>
    decimalMoney(
      value,
      result.data?.currency ?? currency,
      lang === "de" ? "de-AT" : "en-GB",
    );
  const [action, setAction] = useState<
    "issue" | "cancel" | "reverse" | "payments" | "send"
  >();
  const idempotency = useRef(crypto.randomUUID());
  const paymentDate = new Date().toISOString().slice(0, 10);
  return (
    <Modal
      title={result.data?.number || t("invoiceDetails")}
      onClose={onClose}
      wide
    >
      {result.error !== undefined && <ErrorNotice error={result.error} />}{" "}
      {result.loading ? (
        <Loading />
      ) : (
        result.data && (
          <>
            <div className="invoice-heading">
              <div>
                <h3>{result.data.farm_name}</h3>
                <p>
                  {calendarDate(
                    result.data.from_date,
                    lang === "de" ? "de-AT" : "en-GB",
                  )}{" "}
                  —{" "}
                  {calendarDate(
                    result.data.to_date,
                    lang === "de" ? "de-AT" : "en-GB",
                  )}
                </p>
                {result.data.due_on && (
                  <p>
                    {t("dueOn")}:{" "}
                    {calendarDate(
                      result.data.due_on,
                      lang === "de" ? "de-AT" : "en-GB",
                    )}
                  </p>
                )}
              </div>
              <div>
                <Status state={result.data.state} />
                <p className="small">
                  {stateName(result.data.payment_state, lang)}
                  {result.data.overdue && ` · ${t("overdue")}`}
                </p>
              </div>
            </div>
            {result.data.issuer?.invoice_layout === "activity_rate" ? (
              <>
                <InvoiceSummary
                  lines={result.data.summary_lines ?? []}
                  currency={result.data.currency}
                />
                {result.data.issuer.invoice_appendix && (
                  <p className="muted small">{t("invoiceAppendix")}</p>
                )}
              </>
            ) : (
              <div
                className="table-wrap"
                role="region"
                aria-label={t("invoiceDetails")}
                tabIndex={0}
              >
                <table>
                  <thead>
                    <tr>
                      <th>{t("description")}</th>
                      <th>{t("effectiveHourlyRate")}</th>
                      <th>{t("net")}</th>
                    </tr>
                  </thead>
                  <tbody>
                    {(result.data.items ?? []).map((item, i) => (
                      <tr key={item.id || i}>
                        <td>
                          {item.description}
                          {item.pricing?.special_day_policy && (
                            <small className="invoice-policy">
                              {t(
                                item.pricing.special_day_policy.day_type ===
                                  "VACATION"
                                  ? "vacationDay"
                                  : "holidayDay",
                              )}{" "}
                              · +
                              {item.pricing.special_day_policy.mode ===
                              "percentage"
                                ? `${lang === "de" ? item.pricing.special_day_policy.value.replace(".", ",") : item.pricing.special_day_policy.value} %`
                                : `${money(item.pricing.special_day_policy.value)}/h`}{" "}
                              · v{item.pricing.special_day_policy.version}
                            </small>
                          )}
                        </td>
                        <td>
                          {item.pricing?.segments?.map((segment, i) => (
                            <div
                              key={i}
                              title={`${t("pricingRule")}: ${segment.rule_id} v${segment.rule_version}`}
                            >
                              {money(segment.hourly_rate)}
                              {segment.base_hourly_rate && (
                                <small className="block muted">
                                  {t("baseHourlyRate")}:{" "}
                                  {money(segment.base_hourly_rate)}
                                </small>
                              )}
                            </div>
                          )) ||
                            item.hourly_rate ||
                            item.rate ||
                            "—"}
                        </td>
                        <td>{money(item.net || item.amount || "0")}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            {result.data.issuer?.tax_identifier && (
              <p>
                {t("taxIdentifier")}: {result.data.issuer.tax_identifier}
              </p>
            )}
            {result.data.issuer?.invoice_footer && (
              <p style={{ whiteSpace: "pre-wrap" }}>
                {result.data.issuer.invoice_footer}
              </p>
            )}
            <dl className="invoice-totals">
              {(["net", "tax", "total", "paid", "balance"] as const).map(
                (key) => (
                  <div key={key}>
                    <dt>{t(key === "tax" ? "taxAmount" : key)}</dt>
                    <dd>{money(result.data![key])}</dd>
                  </div>
                ),
              )}
            </dl>
            {result.data.payment_state === "credit_balance" && (
              <p className="notice warning">
                {t("externalRefundReview")}:{" "}
                {money(result.data.refund_due || result.data.paid)}
              </p>
            )}
            <InvoiceHistory
              payments={result.data.payments ?? []}
              credits={result.data.credits ?? []}
              currency={result.data.currency}
            />
            <div className="button-group invoice-actions">
              {result.data.state.toUpperCase() === "DRAFT" && (
                <>
                  <button
                    className="button primary"
                    onClick={() => {
                      setAction("issue");
                      idempotency.current = crypto.randomUUID();
                    }}
                  >
                    {t("issue")}
                  </button>
                  <button
                    className="button secondary"
                    onClick={() => setAction("cancel")}
                  >
                    {t("cancelDraft")}
                  </button>
                </>
              )}
              {result.data.state.toUpperCase() === "ISSUED" && (
                <>
                  {smtpAvailable && (
                    <button
                      className="button secondary"
                      onClick={() => {
                        setAction("send");
                        idempotency.current = crypto.randomUUID();
                      }}
                    >
                      {t("sendInvoice")}
                    </button>
                  )}
                  <a
                    className="button secondary"
                    href={`/api/v1/admin/invoices/${id}/pdf`}
                    target="_blank"
                    rel="noreferrer"
                  >
                    <Download size={18} />
                    {t("pdf")}
                  </a>
                  <button
                    className="button primary"
                    onClick={() => {
                      setAction("payments");
                      idempotency.current = crypto.randomUUID();
                    }}
                  >
                    {t("payment")}
                  </button>
                  <button
                    className="text-button danger-text"
                    onClick={() => setAction("reverse")}
                  >
                    {t("reverse")}
                  </button>
                </>
              )}
            </div>
            {result.data.state !== "draft" &&
              result.data.state !== "cancelled" && (
                <InvoiceDelivery
                  id={id}
                  canRetry={smtpAvailable && result.data.state === "issued"}
                />
              )}
            {action && (
              <section className="invoice-action-form">
                <h3>
                  {t(
                    action === "payments"
                      ? "payment"
                      : action === "issue"
                        ? "issue"
                        : action === "reverse"
                          ? "reverse"
                          : action === "send"
                            ? "sendInvoice"
                            : "cancelDraft",
                  )}
                </h3>
                {action === "issue" && <p>{t("confirmIssue")}</p>}
                <ActionForm
                  label={t("save")}
                  onSubmit={async (form) => {
                    await api(
                      `/admin/invoices/${id}/${action}`,
                      "POST",
                      action === "payments"
                        ? {
                            amount: decimalInput(
                              String(form.get("amount") || ""),
                              lang,
                            ),
                            paid_on: form.get("paid_on"),
                            reference: form.get("reference"),
                          }
                        : action === "reverse"
                          ? { reason: form.get("reason") }
                          : {},
                      ["issue", "payments", "send"].includes(action)
                        ? idempotency.current
                        : undefined,
                    );
                    setAction(undefined);
                    await result.refresh();
                    onChange();
                  }}
                >
                  {action === "payments" ? (
                    <>
                      <Field label={t("amount")} hint={t("paymentDecimalHint")}>
                        <input
                          name="amount"
                          required
                          inputMode="decimal"
                          pattern={
                            lang === "de"
                              ? "[0-9]+([.,][0-9]{1,2})?"
                              : "[0-9]+([.][0-9]{1,2})?"
                          }
                        />
                      </Field>
                      <Field label={t("paidOn")}>
                        <input
                          name="paid_on"
                          type="date"
                          required
                          defaultValue={paymentDate}
                        />
                      </Field>
                      <Field label={t("reference")}>
                        <input name="reference" maxLength={500} />
                      </Field>
                    </>
                  ) : action === "reverse" ? (
                    <Field label={t("reason")}>
                      <textarea required name="reason" maxLength={4000} />
                    </Field>
                  ) : null}
                </ActionForm>
              </section>
            )}
          </>
        )
      )}
    </Modal>
  );
}
