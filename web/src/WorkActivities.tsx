import { useState } from "react";
import type { Entity, Work } from "./api";
import { useI18n } from "./i18n";
import { Field } from "./ui";

export function WorkActivities({
  activities,
  entry,
}: {
  activities: Entity[];
  entry?: Work;
}) {
  const { t } = useI18n();
  const original =
    entry?.activity_ids ?? (entry?.activity_id ? [entry.activity_id] : []);
  const [selected, setSelected] = useState(original);
  const [primary, setPrimary] = useState(entry?.activity_id ?? "");
  const choices = activities
    .filter((a) => a.active || original.includes(a.id))
    .map((a) => ({ id: a.id, name: a.name }));
  // Worker catalog responses exclude inactive activities. Keep historical
  // selections visible and editable without losing their saved names.
  for (const [index, id] of original.entries()) {
    if (!choices.some((a) => a.id === id))
      choices.push({
        id,
        name: entry?.activity_names?.[index] ?? entry?.activity_name ?? id,
      });
  }
  function toggle(id: string, checked: boolean) {
    const next = checked
      ? [...selected, id]
      : selected.filter((value) => value !== id);
    setSelected(next);
    if (!next.includes(primary)) setPrimary(next[0] ?? "");
  }
  return (
    <>
      <fieldset className="vacation-question activity-selection">
        <legend>{t("activities")}</legend>
        <p className="muted small">{t("multipleActivitiesHint")}</p>
        <div className="activity-options">
          {choices.map((a) => (
            <label className="check" key={a.id}>
              <input
                type="checkbox"
                name="activity_ids"
                value={a.id}
                checked={selected.includes(a.id)}
                disabled={!selected.includes(a.id) && selected.length >= 50}
                onChange={(e) => toggle(a.id, e.target.checked)}
              />
              {a.name}
            </label>
          ))}
        </div>
      </fieldset>
      <Field label={t("primaryActivity")} hint={t("primaryActivityHint")}>
        <select
          name="activity_id"
          required
          value={primary}
          onChange={(e) => setPrimary(e.target.value)}
        >
          <option value="">{t("select")}</option>
          {choices
            .filter((a) => selected.includes(a.id))
            .map((a) => (
              <option key={a.id} value={a.id}>
                {a.name}
              </option>
            ))}
        </select>
      </Field>
      <Field
        label={t("additionalActivities")}
        hint={t("additionalActivitiesHint")}
      >
        <textarea
          name="additional_activities"
          defaultValue={entry?.additional_activities ?? ""}
          rows={3}
          maxLength={1000}
        />
      </Field>
    </>
  );
}
