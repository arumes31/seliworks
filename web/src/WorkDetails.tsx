import { useState } from "react";
import { api, formInstant } from "./api";
import type { Entity, Work } from "./api";
import { useI18n } from "./i18n";
import { ActionForm, Field, Modal, Status } from "./ui";
import { WorkEditor } from "./WorkEditor";
import { PricingReview } from "./PricingReview";
import { LocalTimeField } from "./LocalTimeField";
export function WorkDetails({
  entry,
  activities,
  onClose,
  onSaved,
  admin = false,
  farms = [],
  workers = [],
}: {
  entry: Work;
  activities: Entity[];
  onClose: () => void;
  onSaved: () => void;
  admin?: boolean;
  farms?: Entity[];
  workers?: Entity[];
}) {
  const { t, date, hours } = useI18n();
  const version = entry.version;
  const [adminEditing, setAdminEditing] = useState(false);
  const [correction, setCorrection] = useState(false);
  const canEdit =
    !admin && ["STOPPED_PENDING_DETAILS", "REJECTED"].includes(entry.state);
  const dayType =
    entry.day_type ??
    (entry.is_vacation_day === true
      ? "VACATION"
      : entry.is_vacation_day === false
        ? "REGULAR"
        : null);
  return (
    <Modal title={entry.farm_name} onClose={onClose} wide>
      <div className="entry-top">
        <Status state={entry.state} />
        <strong>{hours(entry.net_us ?? entry.elapsed_us)}</strong>
      </div>
      <dl className="evidence">
        <div>
          <dt>{t("worker")}</dt>
          <dd>{entry.worker_name}</dd>
        </div>
        {entry.recorded_farm_name && (
          <div>
            <dt>
              {t("original")} · {t("farm")}
            </dt>
            <dd>{entry.recorded_farm_name}</dd>
          </div>
        )}
        {entry.recorded_worker_name && (
          <div>
            <dt>
              {t("original")} · {t("worker")}
            </dt>
            <dd>{entry.recorded_worker_name}</dd>
          </div>
        )}
        <div>
          <dt>{t("original")}</dt>
          <dd>
            {date(entry.started_at)} —{" "}
            {entry.stopped_at ? date(entry.stopped_at) : t("running")}
          </dd>
        </div>
        <div>
          <dt>
            {t("gross")} / {t("breaks")}
          </dt>
          <dd>
            {hours(entry.gross_us)} / {hours(entry.break_us)}
          </dd>
        </div>
        {(entry.effective_start !== entry.started_at ||
          entry.effective_stop !== entry.stopped_at) && (
          <div>
            <dt>{t("effective")}</dt>
            <dd>
              {date(entry.effective_start)} —{" "}
              {entry.effective_stop && date(entry.effective_stop)}
            </dd>
          </div>
        )}
      </dl>
      {entry.state === "REJECTED" && entry.rejection_reason && (
        <div className="notice warning">
          <strong>{t("rejected")}</strong>
          <p>{entry.rejection_reason}</p>
        </div>
      )}
      {admin && entry.stopped_at && (
        <button
          className="button secondary"
          onClick={() => setAdminEditing(!adminEditing)}
        >
          {t(adminEditing ? "cancel" : "adminEditEntry")}
        </button>
      )}
      {canEdit && <div className="notice">{t("pending")}</div>}
      {canEdit || adminEditing ? (
        <WorkEditor
          entry={entry}
          activities={activities}
          farms={farms}
          workers={workers}
          admin={adminEditing}
          onSaved={() => {
            onSaved();
            onClose();
          }}
        />
      ) : (
        <>
          <h3>{entry.activity_name || t("noActivity")}</h3>
          <p className="preserve-lines">{entry.description}</p>
          {entry.location && <p className="muted">{entry.location}</p>}
          {dayType && (
            <p>
              {t("dayType")}:{" "}
              {t(
                dayType === "REGULAR"
                  ? "regularDay"
                  : dayType === "VACATION"
                    ? "vacationDay"
                    : "holidayDay",
              )}
            </p>
          )}
          {entry.additional_deduction_us > 0 && (
            <p>
              {t("deduction")}: {hours(entry.additional_deduction_us)} ·{" "}
              {entry.deduction_reason}
            </p>
          )}
        </>
      )}
      {admin && !adminEditing && (
        <PricingReview id={entry.id} version={entry.version} />
      )}
      {!admin && entry.stopped_at && (
        <button
          className="text-button separated"
          onClick={() => setCorrection(!correction)}
        >
          {t("correction")}
        </button>
      )}
      {correction && (
        <ActionForm
          label={t("correction")}
          onSubmit={async (form) => {
            await api(`/work-sessions/${entry.id}/corrections`, "POST", {
              version,
              requested_start: formInstant(
                form,
                "requested_start",
                entry.effective_start || entry.started_at,
              ),
              requested_stop: formInstant(
                form,
                "requested_stop",
                entry.effective_stop || entry.stopped_at || entry.started_at,
              ),
              reason: form.get("reason"),
            });
            onSaved();
            onClose();
          }}
        >
          <LocalTimeField
            label={t("requestedStart")}
            name="requested_start"
            instant={entry.effective_start || entry.started_at}
          />
          <LocalTimeField
            label={t("requestedStop")}
            name="requested_stop"
            instant={
              entry.effective_stop || entry.stopped_at || entry.started_at
            }
          />
          <Field label={t("reason")}>
            <textarea name="reason" required maxLength={2000} />
          </Field>
          <small className="muted">{t("localTimezone")}</small>
        </ActionForm>
      )}
      {admin && !adminEditing && entry.state === "SUBMITTED" && (
        <div className="approval-actions">
          <ActionForm
            label={t("approve")}
            onSubmit={async () => {
              await api(`/admin/work-sessions/${entry.id}/approve`, "POST", {
                version,
              });
              onSaved();
              onClose();
            }}
          >
            <></>
          </ActionForm>
          <ActionForm
            label={t("reject")}
            onSubmit={async (form) => {
              await api(`/admin/work-sessions/${entry.id}/reject`, "POST", {
                version,
                reason: form.get("reason"),
              });
              onSaved();
              onClose();
            }}
          >
            <Field label={t("reason")}>
              <textarea name="reason" required maxLength={2000} />
            </Field>
          </ActionForm>
        </div>
      )}
    </Modal>
  );
}
