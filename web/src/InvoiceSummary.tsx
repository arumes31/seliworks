import { decimalMoney } from "./api";
import { useI18n } from "./i18n";

export interface InvoiceSummaryLine {
  activity_name: string;
  hourly_rate: string;
  net_us: number;
  net: string;
  day_type: string;
}

export function InvoiceSummary({
  lines,
  currency,
}: {
  lines: InvoiceSummaryLine[];
  currency: string;
}) {
  const { t, lang, hours } = useI18n();
  const money = (value: string) =>
    decimalMoney(value, currency, lang === "de" ? "de-AT" : "en-GB");
  return (
    <div
      className="table-wrap"
      role="region"
      aria-label={t("invoiceSummary")}
      tabIndex={0}
    >
      <table>
        <caption>{t("invoiceSummary")}</caption>
        <thead>
          <tr>
            <th>{t("activity")}</th>
            <th>{t("quantity")}</th>
            <th>{t("effectiveHourlyRate")}</th>
            <th>{t("net")}</th>
          </tr>
        </thead>
        <tbody>
          {lines.map((line, index) => (
            <tr key={index}>
              <td>
                {line.activity_name}
                {(line.day_type === "VACATION" ||
                  line.day_type === "HOLIDAY") && (
                  <small className="block muted">
                    {t(
                      line.day_type === "VACATION"
                        ? "vacationDay"
                        : "holidayDay",
                    )}
                  </small>
                )}
              </td>
              <td>{hours(line.net_us)}</td>
              <td>{money(line.hourly_rate)}/h</td>
              <td>{money(line.net)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
