import { useEffect, useRef, useState } from "react";
import { api } from "./api";
import { useData } from "./hooks";
import { stateName, useI18n } from "./i18n";
import { ActionForm, ErrorNotice, Loading, Status } from "./ui";

interface Delivery {
  id: string;
  state: string;
  recipient: string;
  sent_at: string | null;
  last_error: string;
  total_attempts: number;
  attempt_history: {
    attempt: number;
    state: string;
    error_code: string;
    started_at: string;
    finished_at: string | null;
  }[];
}
export function InvoiceDelivery({
  id,
  canRetry,
}: {
  id: string;
  canRetry: boolean;
}) {
  const { t, lang, date } = useI18n();
  const deliveries = useData<{ items: Delivery[] }>(
    `/admin/invoices/${id}/deliveries`,
  );
  const [retry, setRetry] = useState<Delivery>();
  const key = useRef("");
  useEffect(() => {
    if (
      retry ||
      !deliveries.data?.items.some((item) =>
        ["queued", "sending", "retry"].includes(item.state),
      )
    )
      return;
    const timer = setInterval(() => {
      if (!document.hidden) void deliveries.refresh();
    }, 10_000);
    return () => clearInterval(timer);
  }, [deliveries.data, deliveries.refresh, retry]);
  return (
    <section className="section-block">
      <h3>{t("deliveries")}</h3>
      {deliveries.error !== undefined && (
        <ErrorNotice
          error={deliveries.error}
          retry={() => void deliveries.refresh()}
        />
      )}
      {deliveries.loading ? (
        <Loading />
      ) : !deliveries.data?.items.length ? (
        <p className="muted small">{t("deliveryNotSent")}</p>
      ) : (
        deliveries.data.items.map((delivery) => (
          <div key={delivery.id}>
            <Status state={delivery.state} />
            <p className="small">
              {t("recipient")}: {delivery.recipient}
            </p>
            <p className="small">
              {t("deliveryAttempts")}: {delivery.total_attempts}
            </p>
            {delivery.sent_at && (
              <p className="small">
                {t("deliveryAccepted")}: {date(delivery.sent_at)}
              </p>
            )}
            {delivery.last_error && (
              <p className="small">
                {t("deliveryDiagnostic")}: <code>{delivery.last_error}</code>
              </p>
            )}
            {!!delivery.attempt_history.length && (
              <details>
                <summary>{t("deliveryHistory")}</summary>
                <ul>
                  {delivery.attempt_history.map((attempt) => (
                    <li key={attempt.attempt}>
                      {attempt.attempt} · {stateName(attempt.state, lang)} ·{" "}
                      {date(attempt.started_at)}
                      {attempt.error_code && ` · ${attempt.error_code}`}
                    </li>
                  ))}
                </ul>
              </details>
            )}
            {canRetry &&
              ["failed", "uncertain", "retry"].includes(delivery.state) && (
                <button
                  className="button secondary"
                  onClick={() => {
                    key.current = crypto.randomUUID();
                    setRetry(delivery);
                  }}
                >
                  {t("retryDelivery")}
                </button>
              )}
          </div>
        ))
      )}
      {retry && (
        <div className="invoice-action-form">
          <ActionForm
            label={t("retryDelivery")}
            secondary={
              <button
                type="button"
                className="text-button"
                onClick={() => setRetry(undefined)}
              >
                {t("close")}
              </button>
            }
            onSubmit={async (form) => {
              await api(
                `/admin/deliveries/${retry.id}/retry`,
                "POST",
                { confirm_duplicate_risk: form.get("duplicate_risk") === "on" },
                key.current,
              );
              setRetry(undefined);
              await deliveries.refresh();
            }}
          >
            {retry.state === "uncertain" && (
              <>
                <p className="notice warning">{t("duplicateDeliveryRisk")}</p>
                <label className="check">
                  <input name="duplicate_risk" type="checkbox" required />
                  {t("confirmDuplicateDelivery")}
                </label>
              </>
            )}
          </ActionForm>
        </div>
      )}
    </section>
  );
}
