import { api } from "./api";
import type { Organization } from "./api";
import { useData } from "./hooks";
import { useI18n } from "./i18n";
import { ActionForm, ErrorNotice, Field, Loading, PageHeader } from "./ui";
import { DataManagement } from "./DataManagement";

export function Settings({ onChanged }: { onChanged: () => void }) {
  const { t } = useI18n();
  const result = useData<Organization>("/admin/settings");
  return (
    <>
      <PageHeader title={t("settings")} />
      {result.error !== undefined && <ErrorNotice error={result.error} />}{" "}
      {result.loading ? (
        <Loading />
      ) : (
        result.data && (
          <section className="panel settings-form">
            <p className="notice">{t("taxSetup")}</p>
            <ActionForm
              label={t("save")}
              onSubmit={async (form) => {
                const body: Record<string, unknown> = {
                  version: result.data!.version,
                };
                for (const key of [
                  "name",
                  "timezone",
                  "currency",
                  "address",
                  "email",
                  "bank_details",
                  "tax_percent",
                  "invoice_prefix",
                  "tax_identifier",
                  "invoice_footer",
                  "invoice_layout",
                  "retention_notes",
                ])
                  body[key] = form.get(key);
                for (const key of [
                  "payment_days",
                  "long_session_hours",
                  "security_retention_days",
                ])
                  body[key] = Number(form.get(key));
                body.tax_confirmed = form.get("tax_confirmed") === "on";
                body.invoice_appendix = form.get("invoice_appendix") === "on";
                await api("/admin/settings", "PATCH", body);
                await result.refresh();
                onChanged();
              }}
            >
              <div className="form-grid">
                {(
                  [
                    ["name", "organization"],
                    ["timezone", "timezone"],
                    ["currency", "currency"],
                    ["email", "email"],
                    ["invoice_prefix", "prefix"],
                  ] as const
                ).map(([name, label]) => (
                  <Field label={t(label)} key={name}>
                    <input
                      required
                      name={name}
                      defaultValue={String(result.data![name] ?? "")}
                    />
                  </Field>
                ))}
                {(
                  [
                    ["address", "address"],
                    ["bank_details", "bank"],
                  ] as const
                ).map(([name, label]) => (
                  <Field label={t(label)} key={name}>
                    <textarea name={name} defaultValue={result.data![name]} />
                  </Field>
                ))}
                {(
                  [
                    ["tax_percent", "tax"],
                    ["payment_days", "paymentDays"],
                    ["long_session_hours", "longHours"],
                  ] as const
                ).map(([name, label]) => (
                  <Field label={t(label)} key={name}>
                    <input
                      name={name}
                      type="number"
                      min="0"
                      step={name === "tax_percent" ? "0.01" : "1"}
                      required
                      defaultValue={result.data![name]}
                    />
                  </Field>
                ))}
              </div>
              <p className="muted small">{t("invoicePresentationHint")}</p>
              <div className="form-grid">
                <Field label={t("taxIdentifier")}>
                  <input
                    name="tax_identifier"
                    maxLength={200}
                    defaultValue={result.data.tax_identifier}
                  />
                </Field>
                <Field label={t("invoiceLayout")}>
                  <select
                    name="invoice_layout"
                    defaultValue={result.data.invoice_layout}
                  >
                    <option value="entries">{t("invoiceEntries")}</option>
                    <option value="activity_rate">{t("invoiceSummary")}</option>
                  </select>
                </Field>
                <Field label={t("invoiceFooter")}>
                  <textarea
                    name="invoice_footer"
                    maxLength={2000}
                    defaultValue={result.data.invoice_footer}
                  />
                </Field>
              </div>
              <label className="check">
                <input
                  type="checkbox"
                  name="invoice_appendix"
                  defaultChecked={result.data.invoice_appendix}
                />
                {t("invoiceAppendix")}
              </label>
              <label className="check">
                <input
                  type="checkbox"
                  name="tax_confirmed"
                  defaultChecked={result.data.tax_confirmed}
                />
                {t("taxConfirmed")}
              </label>
              <div className="form-grid section-block">
                <Field label={t("securityRetentionDays")}>
                  <input
                    type="number"
                    name="security_retention_days"
                    min={1}
                    max={3650}
                    step={1}
                    required
                    defaultValue={result.data.security_retention_days}
                  />
                </Field>
                <Field label={t("retentionNotes")}>
                  <textarea
                    name="retention_notes"
                    maxLength={2000}
                    defaultValue={result.data.retention_notes}
                  />
                </Field>
              </div>
            </ActionForm>
          </section>
        )
      )}
      <DataManagement />
    </>
  );
}
