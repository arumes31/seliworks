import { decimalMoney } from "./api";
import { useData } from "./hooks";
import { stateName, useI18n } from "./i18n";
import { ErrorNotice, Loading } from "./ui";

interface PricingReviewData {
  source_version: number;
  frozen: boolean;
  pricing: null | {
    priced: boolean;
    non_billable: boolean;
    currency: string;
    net: string;
    unpriced_reason?: string;
    segments: {
      started_at: string;
      stopped_at: string;
      hourly_rate: string;
      base_hourly_rate?: string;
      rule_id: string;
      rule_version: number;
    }[];
  };
  reservations?: {
    invoice_id: string;
    number: string | null;
    state: string;
    started_at: string;
    stopped_at: string;
  }[];
}

export function PricingReview({
  id,
  version,
}: {
  id: string;
  version: number;
}) {
  const { t, lang, date } = useI18n();
  const result = useData<PricingReviewData>(
    `/admin/work-sessions/${id}/pricing`,
    version,
  );
  if (result.error !== undefined)
    return (
      <ErrorNotice error={result.error} retry={() => void result.refresh()} />
    );
  if (result.loading) return <Loading />;
  const review = result.data;
  if (!review) return null;
  if (review.source_version !== version)
    return <p className="notice warning">{t("conflict")}</p>;
  const pricing = review.pricing;
  const money = (value: string) =>
    decimalMoney(
      value,
      pricing?.currency || "EUR",
      lang === "de" ? "de-AT" : "en-GB",
    );
  return (
    <section className="section-block">
      <h3>{t(review.frozen ? "frozenPricing" : "pricingPreview")}</h3>
      {!review.frozen && (
        <p className="muted small">{t("pricingPreviewHint")}</p>
      )}
      {!pricing ? (
        <p className="notice">{t("pending")}</p>
      ) : (
        <>
          {!pricing.priced ? (
            <p className="notice warning">
              {t(
                pricing.unpriced_reason?.endsWith("_POLICY_REQUIRED")
                  ? "surchargeRequired"
                  : "rateRequired",
              )}{" "}
              <a href="#rates">{t("rates")}</a>
            </p>
          ) : (
            <p>
              {pricing.non_billable
                ? t("nonBillable")
                : `${t("net")}: ${money(pricing.net)}`}
            </p>
          )}
          {pricing.segments.map((segment, index) => (
            <div key={index} className="separated small">
              <p>
                {date(segment.started_at)} — {date(segment.stopped_at)}
              </p>
              <p>
                {t("effectiveHourlyRate")}: {money(segment.hourly_rate)}/h
                {segment.base_hourly_rate &&
                  ` · ${t("baseHourlyRate")}: ${money(segment.base_hourly_rate)}/h`}
              </p>
              <p className="muted">
                {t("pricingRule")}: {segment.rule_id} · v{segment.rule_version}
              </p>
            </div>
          ))}
        </>
      )}
      {!!review.reservations?.length && (
        <>
          <h4>{t("billing")}</h4>
          {review.reservations.map((claim, index) => (
            <p className="small" key={index}>
              {stateName(claim.state, lang)} · {claim.number || t("reserved")} ·{" "}
              {date(claim.started_at)} — {date(claim.stopped_at)}
            </p>
          ))}
        </>
      )}
    </section>
  );
}
