export type ReportPreset = "day" | "week" | "month" | "year";

// UTC arithmetic here advances calendar dates, not authoritative elapsed time.
export function reportPeriod(
  preset: ReportPreset,
  timezone: string,
  now = new Date(),
) {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: timezone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(now);
  const value = (kind: string) =>
    Number(parts.find((part) => part.type === kind)!.value);
  const start = new Date(
    Date.UTC(value("year"), value("month") - 1, value("day")),
  );
  if (preset === "week")
    start.setUTCDate(start.getUTCDate() - ((start.getUTCDay() + 6) % 7));
  if (preset === "month" || preset === "year") start.setUTCDate(1);
  if (preset === "year") start.setUTCMonth(0);
  const stop = new Date(start);
  if (preset === "year") stop.setUTCFullYear(stop.getUTCFullYear() + 1);
  else if (preset === "month") stop.setUTCMonth(stop.getUTCMonth() + 1);
  else stop.setUTCDate(stop.getUTCDate() + (preset === "week" ? 7 : 1));
  return {
    from: start.toISOString().slice(0, 10),
    to: stop.toISOString().slice(0, 10),
  };
}
