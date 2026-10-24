import { useEffect, useRef, useState } from "react";
import { Download } from "lucide-react";
import { APIError } from "./api";
import type { Entity } from "./api";
import { useI18n } from "./i18n";
import { reportPeriod } from "./report-periods";
import { ActionForm, Field, Modal } from "./ui";

export function TimesheetExport({ workers }: { workers?: Entity[] }) {
  const { t, lang, timezone } = useI18n();
  const team = workers !== undefined;
  const [all, setAll] = useState(true);
  const [selected, setSelected] = useState<string[]>([]);
  const title = t(team ? "teamTimesheets" : "personalTimesheet");
  const [open, setOpen] = useState(false);
  const controller = useRef<AbortController | null>(null);
  useEffect(() => () => controller.current?.abort(), []);
  const period = reportPeriod("month", timezone);
  const lastDay = new Date(period.to + "T00:00:00Z");
  lastDay.setUTCDate(lastDay.getUTCDate() - 1);
  const [from, setFrom] = useState(period.from);
  const [to, setTo] = useState(lastDay.toISOString().slice(0, 10));

  async function download() {
    if (team && !all && selected.length === 0)
      throw new APIError(
        422,
        "Choose at least one employee",
        "TIMESHEET_WORKERS",
      );
    const current = new AbortController();
    controller.current = current;
    const timeout = setTimeout(() => current.abort(), 20000);
    try {
      const query = new URLSearchParams({ from, to, lang });
      if (team) {
        if (all) query.set("all", "true");
        else selected.forEach((id) => query.append("worker_id", id));
      }
      const endpoint = team ? "/admin/timesheets.pdf" : "/me/timesheet.pdf";
      const response = await fetch("/api/v1" + endpoint + "?" + query, {
        credentials: "same-origin",
        cache: "no-store",
        signal: current.signal,
        headers: { Accept: "application/pdf" },
      });
      if (!response.ok) {
        const value = await response.json().catch(() => null);
        throw new APIError(
          response.status,
          value?.error?.message ?? `HTTP ${response.status}`,
          value?.error?.code ?? "http_error",
        );
      }
      if (!response.headers.get("content-type")?.includes("application/pdf"))
        throw new APIError(
          response.status,
          "Unexpected PDF response",
          "INVALID_RESPONSE",
        );
      const blob = await response.blob();
      if (current.signal.aborted) return;
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = `timesheet-${from}-${to}.pdf`;
      document.body.append(link);
      link.click();
      link.remove();
      setTimeout(() => URL.revokeObjectURL(url), 60000);
    } catch (error) {
      if (
        error instanceof TypeError ||
        (error instanceof Error && error.name === "AbortError")
      )
        throw new APIError(
          0,
          "The PDF could not be downloaded. Check your connection and try again.",
          "NETWORK_READ",
        );
      throw error;
    } finally {
      clearTimeout(timeout);
      if (controller.current === current) controller.current = null;
    }
  }
  return (
    <>
      <button className="button secondary" onClick={() => setOpen(true)}>
        <Download size={18} />
        {title}
      </button>
      {open && (
        <Modal
          title={title}
          onClose={() => {
            controller.current?.abort();
            setOpen(false);
          }}
        >
          <p>{t(team ? "teamTimesheetHint" : "timesheetHint")}</p>
          <ActionForm
            label={t("downloadTimesheet")}
            onSubmit={download}
            clearErrorOnInput
          >
            {team && (
              <fieldset>
                <legend>{t("workers")}</legend>
                <label className="check">
                  <input
                    type="checkbox"
                    checked={all}
                    onChange={(e) => setAll(e.target.checked)}
                  />
                  {t("timesheetAllWorkers")}
                </label>
                {!all &&
                  workers
                    .filter((w) => w.timesheet_eligible || w.is_worker)
                    .map((worker) => (
                      <label className="check" key={worker.id}>
                        <input
                          type="checkbox"
                          checked={selected.includes(worker.id)}
                          onChange={(e) =>
                            setSelected(
                              e.target.checked
                                ? [...selected, worker.id]
                                : selected.filter((id) => id !== worker.id),
                            )
                          }
                        />
                        {worker.name}
                        {worker.email ? ` (${worker.email})` : ""}
                        {!worker.active ? ` · ${t("inactive")}` : ""}
                      </label>
                    ))}
              </fieldset>
            )}
            <div className="filters">
              <Field label={t("from")}>
                <input
                  name="from"
                  type="date"
                  required
                  value={from}
                  max={to || undefined}
                  onChange={(e) => setFrom(e.target.value)}
                />
              </Field>
              <Field label={t("timesheetThrough")}>
                <input
                  name="to"
                  type="date"
                  required
                  value={to}
                  min={from || undefined}
                  onChange={(e) => setTo(e.target.value)}
                />
              </Field>
            </div>
            <p className="muted">{timezone}</p>
          </ActionForm>
        </Modal>
      )}
    </>
  );
}
