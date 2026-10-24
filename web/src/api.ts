import { localizedAPIError } from "./error-messages.ts";

export interface User {
  id: string;
  name: string;
  email: string;
  is_admin: boolean;
  is_worker: boolean;
  active: boolean;
  language: "en" | "de";
  theme: "light" | "dark" | "system";
  version: number;
  totp_enabled?: boolean;
  passkey_count?: number;
  passkey_remind_after?: string | null;
}
export interface Organization {
  name: string;
  timezone: string;
  currency: string;
  address: string;
  email: string;
  bank_details: string;
  tax_percent: string;
  tax_confirmed: boolean;
  invoice_prefix: string;
  tax_identifier: string;
  invoice_footer: string;
  invoice_layout: "entries" | "activity_rate";
  invoice_appendix: boolean;
  security_retention_days: number;
  retention_notes: string;
  payment_days: number;
  long_session_hours: number;
  version: number;
}
export interface Auth {
  smtp_available?: boolean;
  user: User;
  organization: Organization;
  csrf_token: string;
  totp_available?: boolean;
}
export interface Entity {
  running?: boolean;
  id: string;
  name: string;
  active: boolean;
  version: number;
  email?: string;
  address?: string;
  is_admin?: boolean;
  is_worker?: boolean;
  timesheet_eligible?: boolean;
  farm_ids?: string[];
  assignment_revision?: string;
  assign_all_workers?: boolean;
  worker_ids?: string[];
}
export interface Break {
  started_at: string;
  stopped_at: string;
}
export type DayType = "REGULAR" | "VACATION" | "HOLIDAY";
export interface Work {
  pricing_status?: string;
  day_type?: DayType | null;
  is_vacation_day: boolean | null;
  additional_deduction_us: number;
  deduction_reason: string;
  original_farm_name?: string;
  recorded_farm_id?: string;
  recorded_worker_id?: string;
  recorded_farm_name?: string;
  recorded_worker_name?: string;
  active_pause?: {
    id: string;
    started_at: string;
    planned_stop: string | null;
  } | null;
  rejection_reason?: string;
  id: string;
  worker_id: string;
  worker_name: string;
  farm_id: string;
  farm_name: string;
  activity_id: string;
  activity_name: string;
  started_at: string;
  stopped_at: string | null;
  effective_start: string;
  effective_stop: string | null;
  state: string;
  description: string;
  location: string;
  version: number;
  server_now: string;
  gross_us: number;
  break_us: number;
  net_us: number;
  elapsed_us: number;
  non_billable: boolean;
  breaks: Break[];
}
export interface List<T> {
  items: T[];
  total: number;
}
export class APIError extends Error {
  status: number;
  code: string;
  constructor(status: number, message: string, code: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}
let csrf = "";
let pendingWrites = 0;
export function hasPendingWrites() {
  return pendingWrites > 0;
}
export function setCSRF(value: string) {
  csrf = value;
}
export async function api<T>(
  path: string,
  method = "GET",
  body?: unknown,
  key?: string,
): Promise<T> {
  const payload = body === undefined ? undefined : JSON.stringify(body);
  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), 20000);
  if (method !== "GET") pendingWrites++;
  try {
    const response = await fetch("/api/v1" + path, {
      method,
      credentials: "same-origin",
      cache: "no-store",
      signal: controller.signal,
      headers: {
        Accept: "application/json",
        ...(body !== undefined ? { "Content-Type": "application/json" } : {}),
        ...(method !== "GET" ? { "X-CSRF-Token": csrf } : {}),
        ...(key ? { "Idempotency-Key": key } : {}),
      },
      body: payload,
    });
    let value: {
      error?: { message?: string; code?: string };
    } | null = null;
    if (response.status !== 204) {
      try {
        value = await response.json();
      } catch (error) {
        if (!(error instanceof SyntaxError)) throw error;
        if (response.ok)
          throw new APIError(
            response.status,
            "The server returned an unreadable response. Refresh the view before trying again.",
            "INVALID_RESPONSE",
          );
      }
    }
    if (!response.ok)
      throw new APIError(
        response.status,
        value?.error?.message ?? `HTTP ${response.status}`,
        value?.error?.code ?? "http_error",
      );
    return value as T;
  } catch (error) {
    if (
      error instanceof TypeError ||
      (error instanceof Error && error.name === "AbortError")
    )
      throw new APIError(
        0,
        method === "GET"
          ? "Data could not be loaded. Check your connection and refresh the view."
          : "The connection was interrupted or timed out. Success is unconfirmed; check the current state before trying again.",
        method === "GET" ? "NETWORK_READ" : "NETWORK_WRITE",
      );
    throw error;
  } finally {
    if (method !== "GET") pendingWrites--;
    clearTimeout(timeout);
  }
}
export function errorText(
  error: unknown,
  language = typeof document === "undefined"
    ? "de"
    : document.documentElement.lang,
) {
  if (error instanceof APIError)
    return localizedAPIError(error.code, error.message, language);
  return error instanceof Error ? error.message : String(error);
}
// Currency values remain decimal strings; grouping and currency placement never coerce them to binary floats.
export function decimalInput(value: string, language: string) {
  return language === "de" ? value.trim().replace(",", ".") : value.trim();
}
export function decimalMoney(value: string, currency: string, locale: string) {
  const match = /^(-?)(\d+)(?:\.(\d+))?$/.exec(value || "0");
  if (!match) return value;
  const whole = new Intl.NumberFormat(locale, {
    maximumFractionDigits: 0,
  }).format(BigInt(match[2]!));
  const fraction = (match[3] ?? "").padEnd(2, "0");
  return new Intl.NumberFormat(locale, {
    style: "currency",
    currency,
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
    .formatToParts(match[1] ? -0 : 0)
    .map((part) =>
      part.type === "integer"
        ? whole
        : part.type === "fraction"
          ? fraction
          : part.value,
    )
    .join("");
}
export function elapsed(us: number) {
  const seconds = Math.max(0, Math.floor(us / 1e6));
  return `${Math.floor(seconds / 3600)
    .toString()
    .padStart(2, "0")}:${Math.floor((seconds % 3600) / 60)
    .toString()
    .padStart(2, "0")}:${(seconds % 60).toString().padStart(2, "0")}`;
}
export function calendarDate(value: string, locale: string) {
  return new Intl.DateTimeFormat(locale, {
    dateStyle: "medium",
    timeZone: "UTC",
  }).format(new Date(value + "T00:00:00Z"));
}
export function localInput(instant: string, withSeconds = false) {
  const d = new Date(instant);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}T${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}${withSeconds ? ":" + String(d.getSeconds()).padStart(2, "0") : ""}`;
}
function normalizedLocal(value: string) {
  return value.length === 16 ? value + ":00" : value;
}
export function localTimeCandidates(value: string): string[] {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2})?$/.test(value)) return [];
  const desired = normalizedLocal(value);
  const anchor = new Date(value);
  if (!Number.isFinite(anchor.getTime())) return [];
  const wall = new Date(desired + "Z").getTime();
  const offsets = new Set<number>();
  for (let hours = -48; hours <= 48; hours += 6)
    offsets.add(
      new Date(anchor.getTime() + hours * 3600000).getTimezoneOffset(),
    );
  const instants = new Set<number>([anchor.getTime()]);
  for (const offset of offsets) instants.add(wall + offset * 60000);
  return [...instants]
    .filter(
      (instant) =>
        localInput(new Date(instant).toISOString(), true) === desired,
    )
    .sort((a, b) => a - b)
    .map((instant) => new Date(instant).toISOString());
}
export function utcInput(value: string, choice: string | null = null) {
  const candidates = localTimeCandidates(value);
  if (candidates.length === 0)
    throw new APIError(
      422,
      "This local time does not exist. Choose a valid time around the clock change.",
      "NONEXISTENT_LOCAL_TIME",
    );
  if (choice && candidates.includes(choice)) return choice;
  if (candidates.length > 1)
    throw new APIError(
      422,
      "This local time occurs twice. Choose its UTC offset explicitly.",
      "AMBIGUOUS_LOCAL_TIME",
    );
  return candidates[0]!;
}
export function formInstant(form: FormData, name: string, original?: string) {
  const value = String(form.get(name) || "");
  const choice = String(form.get(name + "_offset") || "");
  if (
    original &&
    normalizedLocal(value) === localInput(original, true) &&
    (!choice ||
      choice.slice(0, 19) === new Date(original).toISOString().slice(0, 19))
  )
    return original;
  return utcInput(value, choice);
}
