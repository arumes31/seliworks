import { useState } from "react";
import { api, decimalInput } from "./api";
import { useData } from "./hooks";
import { useI18n } from "./i18n";
import { ActionForm, ErrorNotice, Field, Loading } from "./ui";
interface Policy {
  day_type: "VACATION" | "HOLIDAY";
  mode: null | "percentage" | "hourly";
  value: null | string;
  version: number;
}
export function SpecialDayPricing() {
  const { t } = useI18n();
  const result = useData<{ items: Policy[] }>("/admin/special-day-pricing");
  return (
    <section className="section-block special-day-pricing">
      <h2>{t("specialDayPricing")}</h2>
      <p className="muted small">{t("specialDayHint")}</p>
      {result.error !== undefined && (
        <ErrorNotice error={result.error} retry={() => void result.refresh()} />
      )}{" "}
      {result.loading ? (
        <Loading />
      ) : (
        <div className="special-day-grid">
          {result.data?.items.map((policy) => (
            <PolicyForm
              key={`${policy.day_type}:${policy.version}`}
              policy={policy}
              onSaved={result.refresh}
            />
          ))}
        </div>
      )}
      <p className="muted small separated">{t("specialDayHistory")}</p>
    </section>
  );
}
function PolicyForm({
  policy,
  onSaved,
}: {
  policy: Policy;
  onSaved: () => Promise<void>;
}) {
  const { t, currency, lang } = useI18n();
  const symbol =
    new Intl.NumberFormat(lang === "de" ? "de-AT" : "en-GB", {
      style: "currency",
      currency,
    })
      .formatToParts(0)
      .find((part) => part.type === "currency")?.value ?? currency;
  const [mode, setMode] = useState(policy.mode ?? "");
  return (
    <section className="panel">
      <h3>
        {t(policy.day_type === "VACATION" ? "vacationDay" : "holidayDay")}
      </h3>
      <ActionForm
        label={t("save")}
        onSubmit={async (form) => {
          await api(`/admin/special-day-pricing/${policy.day_type}`, "PATCH", {
            version: policy.version,
            mode: mode || null,
            value: mode ? decimalInput(String(form.get("value")), lang) : null,
          });
          await onSaved();
        }}
      >
        <Field label={t("surchargeMode")}>
          <select
            name="mode"
            value={mode}
            onChange={(e) => setMode(e.target.value)}
          >
            <option value="">{t("notConfigured")}</option>
            <option value="percentage">{t("percentageExtra")} (%)</option>
            <option value="hourly">
              {t("hourlyExtra")} ({symbol}/h)
            </option>
          </select>
        </Field>
        <Field
          label={`${t("surchargeValue")}${mode === "percentage" ? " (%)" : mode === "hourly" ? ` (${symbol}/h)` : ""}`}
          hint={t("decimalHint")}
        >
          <input
            name="value"
            type="text"
            inputMode="decimal"
            pattern={
              lang === "de"
                ? "[0-9]+([.,][0-9]{1,4})?"
                : "[0-9]+([.][0-9]{1,4})?"
            }
            required={!!mode}
            disabled={!mode}
            defaultValue={
              lang === "de"
                ? (policy.value ?? "").replace(".", ",")
                : (policy.value ?? "")
            }
            maxLength={32}
          />
        </Field>
      </ActionForm>
    </section>
  );
}
