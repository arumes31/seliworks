import { useEffect, useRef, useState } from "react";
import { Download } from "lucide-react";
import { APIError } from "./api";
import { useI18n } from "./i18n";
import { reportPeriod } from "./report-periods";
import { ActionForm, Field, Modal } from "./ui";

export function TimesheetExport() {
  const { t, lang, timezone } = useI18n();
  const [open, setOpen] = useState(false);
  const controller = useRef<AbortController | null>(null);
  useEffect(() => () => controller.current?.abort(), []);
  const period = reportPeriod("month", timezone);
  const lastDay = new Date(period.to + "T00:00:00Z");
  lastDay.setUTCDate(lastDay.getUTCDate() - 1);
  const [from, setFrom] = useState(period.from);
  const [to, setTo] = useState(lastDay.toISOString().slice(0, 10));

  async function download() {
    const current = new AbortController();
    controller.current = current;
    const timeout = setTimeout(() => current.abort(), 20000);
    try {
      const query = new URLSearchParams({ from, to, lang });
      const response = await fetch("/api/v1/me/timesheet.pdf?" + query, {
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
        {t("personalTimesheet")}
      </button>
      {open && (
        <Modal
          title={t("personalTimesheet")}
          onClose={() => {
            controller.current?.abort();
            setOpen(false);
          }}
        >
          <p>{t("timesheetHint")}</p>
          <ActionForm
            label={t("downloadTimesheet")}
            onSubmit={download}
            clearErrorOnInput
          >
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
