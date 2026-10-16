import { useRef, useState } from "react";
import { Download } from "lucide-react";
import { api } from "./api";
import { useData } from "./hooks";
import { useI18n } from "./i18n";
import { ActionForm, ErrorNotice, Loading, Modal } from "./ui";

interface RetentionPreview {
  version: number;
  security_retention_days: number;
  cutoff: string;
  candidates: {
    auth_sessions: number;
    invitations: number;
    auth_limits: number;
  };
}

export function DataManagement() {
  const { t, date } = useI18n();
  const preview = useData<RetentionPreview>("/admin/retention");
  const [confirming, setConfirming] = useState(false);
  const key = useRef("");
  return (
    <section className="panel section-block">
      <h2>{t("dataManagement")}</h2>
      <p className="muted small">{t("exportBusinessHint")}</p>
      <a className="button secondary" href="/api/v1/admin/export">
        <Download size={18} />
        {t("exportBusiness")}
      </a>
      <h3 className="section-block">{t("securityCleanup")}</h3>
      <p className="muted small">{t("retentionHint")}</p>
      {preview.error !== undefined && (
        <ErrorNotice
          error={preview.error}
          retry={() => void preview.refresh()}
        />
      )}
      {preview.loading ? (
        <Loading />
      ) : (
        preview.data && (
          <>
            <p>
              {t("olderThan")}: {date(preview.data.cutoff)}
            </p>
            <dl className="evidence">
              {(
                [
                  ["auth_sessions", "expiredSessions"],
                  ["invitations", "expiredInvitations"],
                  ["auth_limits", "oldLoginCounters"],
                ] as const
              ).map(([field, label]) => (
                <div key={field}>
                  <dt>{t(label)}</dt>
                  <dd>{preview.data!.candidates[field]}</dd>
                </div>
              ))}
            </dl>
            <div className="button-group">
              <button
                className="button secondary"
                onClick={() => void preview.refresh()}
              >
                {t("refreshPreview")}
              </button>
              <button
                className="button secondary"
                disabled={
                  !Object.values(preview.data.candidates).some(
                    (value) => value > 0,
                  )
                }
                onClick={() => {
                  key.current = crypto.randomUUID();
                  setConfirming(true);
                }}
              >
                {t("securityCleanup")}
              </button>
            </div>
          </>
        )
      )}
      {confirming && preview.data && (
        <Modal
          title={t("securityCleanup")}
          onClose={() => setConfirming(false)}
        >
          <p>{t("retentionHint")}</p>
          <ActionForm
            label={t("securityCleanup")}
            onSubmit={async () => {
              await api(
                "/admin/retention/cleanup",
                "POST",
                { version: preview.data!.version, confirmed: true },
                key.current,
              );
              setConfirming(false);
              await preview.refresh();
            }}
          >
            <label className="check">
              <input type="checkbox" required />
              {t("confirmSecurityCleanup")}
            </label>
          </ActionForm>
        </Modal>
      )}
    </section>
  );
}
