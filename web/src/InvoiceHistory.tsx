import { calendarDate, decimalMoney } from "./api";
import { useI18n } from "./i18n";

export interface InvoicePayment {
  id: string;
  amount: string;
  paid_on: string;
  reference: string;
}
export interface InvoiceCredit {
  id: string;
  number: string;
  total: string;
  reason: string;
}
export function InvoiceHistory({
  payments,
  credits,
  currency,
}: {
  payments: InvoicePayment[];
  credits: InvoiceCredit[];
  currency: string;
}) {
  const { t, lang } = useI18n();
  const locale = lang === "de" ? "de-AT" : "en-GB";
  const money = (value: string) => decimalMoney(value, currency, locale);
  return (
    <>
      {!!payments.length && (
        <section className="section-block">
          <h3>{t("paymentHistory")}</h3>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>{t("paidOn")}</th>
                  <th>{t("amount")}</th>
                  <th>{t("reference")}</th>
                </tr>
              </thead>
              <tbody>
                {payments.map((payment) => (
                  <tr key={payment.id}>
                    <td>{calendarDate(payment.paid_on, locale)}</td>
                    <td>{money(payment.amount)}</td>
                    <td>{payment.reference || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}
      {!!credits.length && (
        <section className="section-block">
          <h3>{t("creditHistory")}</h3>
          {credits.map((credit) => (
            <p key={credit.id}>
              {credit.number} · {money(credit.total)} · {credit.reason}
            </p>
          ))}
        </section>
      )}
    </>
  );
}
