import { useState } from "react";
import { Plus, Trash2 } from "lucide-react";
import { api, formInstant } from "./api";
import type { Break, DayType, Entity, Work } from "./api";
import { useI18n } from "./i18n";
import { ActionForm, Field } from "./ui";
import { LocalTimeField } from "./LocalTimeField";

export function WorkEditor({
  entry,
  activities,
  farms = [],
  workers = [],
  admin = false,
  onSaved,
}: {
  entry: Work;
  activities: Entity[];
  farms?: Entity[];
  workers?: Entity[];
  admin?: boolean;
  onSaved: () => void;
}) {
  const { t } = useI18n();
  const [version, setVersion] = useState(entry.version);
  const [breaks, setBreaks] = useState<Break[]>(entry.breaks ?? []);
  const [dayType, setDayType] = useState<DayType | undefined>(
    entry.day_type ??
      (entry.is_vacation_day === true
        ? "VACATION"
        : entry.is_vacation_day === false
          ? "REGULAR"
          : undefined),
  );
  const [deduction, setDeduction] = useState(
    (entry.additional_deduction_us ?? 0) / 60e6,
  );
  const [detailsSaved, setDetailsSaved] = useState(false);
  async function save(form: FormData) {
    if (!dayType) throw new Error(t("dayTypeRequired"));
    const originalStart = entry.effective_start || entry.started_at;
    const originalStop = entry.effective_stop || entry.stopped_at || "";
    const startISO = admin
      ? formInstant(form, "effective_start", originalStart)
      : originalStart;
    const stopISO = admin
      ? formInstant(form, "effective_stop", originalStop)
      : originalStop;
    const intervals = breaks
      .map((b) => ({ ...b }))
      .sort((a, b) => Date.parse(a.started_at) - Date.parse(b.started_at));
    const start = Date.parse(startISO);
    const stop = Date.parse(stopISO);
    let breakUS = 0;
    for (let i = 0; i < intervals.length; i++) {
      const b = intervals[i]!;
      if (
        Date.parse(b.started_at) < start ||
        Date.parse(b.stopped_at) > stop ||
        Date.parse(b.started_at) >= Date.parse(b.stopped_at) ||
        (i > 0 &&
          Date.parse(intervals[i - 1]!.stopped_at) > Date.parse(b.started_at))
      )
        throw new Error(t("invalidBreak"));
      breakUS += (Date.parse(b.stopped_at) - Date.parse(b.started_at)) * 1000;
    }
    const deductionUS = Math.round(deduction * 60e6);
    if (
      !Number.isSafeInteger(deductionUS) ||
      deductionUS < 0 ||
      stop <= start ||
      breakUS + deductionUS > (stop - start) * 1000
    )
      throw new Error(t("invalidNet"));
    const body = {
      version,
      activity_id: form.get("activity_id"),
      description: form.get("description"),
      location: form.get("location"),
      non_billable: form.get("non_billable") === "on",
      breaks: intervals,
      day_type: dayType,
      is_vacation_day: dayType === "VACATION",
      additional_deduction_us: deductionUS,
      deduction_reason: form.get("deduction_reason") || "",
      ...(admin
        ? {
            effective_start: startISO,
            effective_stop: stopISO,
            farm_id: form.get("farm_id"),
            worker_id: form.get("worker_id"),
            reason: form.get("reason"),
          }
        : {}),
    };
    const saved = await api<Work>(
      admin
        ? `/admin/work-sessions/${entry.id}`
        : `/work-sessions/${entry.id}/details`,
      "PATCH",
      body,
    );
    setVersion(saved.version);
    setDetailsSaved(true);
    if (!admin)
      await api(`/work-sessions/${entry.id}/submit`, "POST", {
        version: saved.version,
      });
    onSaved();
  }
  return (
    <ActionForm label={t(admin ? "save" : "submit")} onSubmit={save}>
      {detailsSaved && <div className="notice">{t("savedDraft")}</div>}
      {admin && (
        <>
          <div className="notice">{t("adminEditHint")}</div>
          <Field label={t("farm")}>
            <select name="farm_id" required defaultValue={entry.farm_id}>
              {farms
                .filter((f) => f.active || f.id === entry.farm_id)
                .map((f) => (
                  <option key={f.id} value={f.id}>
                    {f.name}
                  </option>
                ))}
            </select>
            <small className="muted">{t("farmTransferHint")}</small>
          </Field>
          <Field label={t("worker")}>
            <select name="worker_id" required defaultValue={entry.worker_id}>
              {workers
                .filter((w) => w.is_worker || w.id === entry.worker_id)
                .map((w) => (
                  <option key={w.id} value={w.id}>
                    {w.name}
                    {w.active ? "" : ` · ${t("inactive")}`}
                  </option>
                ))}
            </select>
          </Field>
          <div className="form-grid">
            <LocalTimeField
              label={t("startTime")}
              name="effective_start"
              instant={entry.effective_start || entry.started_at}
            />
            <LocalTimeField
              label={t("stopTime")}
              name="effective_stop"
              instant={
                entry.effective_stop || entry.stopped_at || entry.started_at
              }
            />
          </div>
        </>
      )}
      <Field label={t("activity")}>
        <select
          name="activity_id"
          required
          defaultValue={entry.activity_id || ""}
        >
          <option value="">{t("select")}</option>
          {activities
            .filter((a) => a.active || a.id === entry.activity_id)
            .map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
        </select>
      </Field>
      <Field label={t("description")}>
        <textarea
          name="description"
          required
          defaultValue={entry.description}
          rows={4}
          maxLength={4000}
        />
      </Field>
      <Field label={t("location")}>
        <input name="location" defaultValue={entry.location} maxLength={300} />
      </Field>
      <fieldset className="vacation-question">
        <legend>{t("dayType")}</legend>
        <p className="muted small">{t("dayTypeHint")}</p>
        <div className="button-group">
          {(["REGULAR", "VACATION", "HOLIDAY"] as const).map((type) => (
            <label className="check" key={type}>
              <input
                required
                name="day_type"
                type="radio"
                value={type}
                checked={dayType === type}
                onChange={() => setDayType(type)}
              />
              {t(
                type === "REGULAR"
                  ? "regularDay"
                  : type === "VACATION"
                    ? "vacationDay"
                    : "holidayDay",
              )}
            </label>
          ))}
        </div>
      </fieldset>
      <Field label={t("additionalDeduction")}>
        <input
          name="additional_deduction_minutes"
          type="number"
          inputMode="decimal"
          min={0}
          step="0.01"
          required
          value={deduction}
          onChange={(e) => setDeduction(e.target.valueAsNumber)}
        />
      </Field>
      <Field label={t("deductionReason")}>
        <textarea
          name="deduction_reason"
          defaultValue={entry.deduction_reason}
          required={deduction > 0}
          maxLength={2000}
        />
      </Field>
      <label className="check">
        <input
          type="checkbox"
          name="non_billable"
          defaultChecked={entry.non_billable}
        />
        {t("nonBillable")}
      </label>
      <div className="break-heading">
        <h3>{t("breaks")}</h3>
        <button
          type="button"
          className="text-button"
          onClick={() =>
            setBreaks([
              ...breaks,
              {
                started_at: entry.effective_start || entry.started_at,
                stopped_at:
                  entry.effective_stop || entry.stopped_at || entry.started_at,
              },
            ])
          }
        >
          <Plus size={16} />
          {t("addBreak")}
        </button>
      </div>
      {breaks.map((b, i) => (
        <div className="break-row" key={i}>
          <LocalTimeField
            name={`break_${i}_start`}
            label={t("startTime")}
            instant={b.started_at}
            onInstantChange={(instant) =>
              setBreaks(
                breaks.map((v, j) =>
                  j === i ? { ...v, started_at: instant } : v,
                ),
              )
            }
          />
          <LocalTimeField
            name={`break_${i}_stop`}
            label={t("stopTime")}
            instant={b.stopped_at}
            onInstantChange={(instant) =>
              setBreaks(
                breaks.map((v, j) =>
                  j === i ? { ...v, stopped_at: instant } : v,
                ),
              )
            }
          />
          <button
            type="button"
            className="icon-button"
            aria-label={t("deleteBreak")}
            onClick={() => setBreaks(breaks.filter((_, j) => i !== j))}
          >
            <Trash2 size={18} />
          </button>
        </div>
      ))}
      <small className="muted">{t("localTimezone")}</small>
      {admin && (
        <Field label={t("reason")}>
          <textarea name="reason" required maxLength={2000} />
        </Field>
      )}
    </ActionForm>
  );
}
