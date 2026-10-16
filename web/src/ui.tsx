import {
  cloneElement,
  isValidElement,
  useEffect,
  useId,
  useRef,
  useState,
} from "react";
import type { FormEvent, ReactNode } from "react";
import {
  ArrowRight,
  Check,
  ChevronLeft,
  ChevronRight,
  LoaderCircle,
  RefreshCw,
  Sunrise,
  X,
} from "lucide-react";
import { APIError, errorText } from "./api";
import { stateName, useI18n } from "./i18n";
export function Brand({ compact = false }: { compact?: boolean }) {
  return (
    <div className="brand">
      <svg viewBox="0 0 44 44" aria-hidden="true">
        <path d="M8 24a14 14 0 0 1 28 0" />
        <path d="M22 3v4M6 9l4 4M38 9l-4 4M3 24h38M5 31c10-4 24-4 34 0M8 38c9-3 19-3 28 0" />
      </svg>
      {!compact && (
        <span>
          Seli<span>Works</span>
        </span>
      )}
    </div>
  );
}
export function Loading() {
  const { t } = useI18n();
  return (
    <div className="loading" role="status">
      <LoaderCircle className="spin" />
      {t("loading")}
    </div>
  );
}
export function ErrorNotice({
  error,
  retry,
}: {
  error: unknown;
  retry?: () => void;
}) {
  const { t, lang } = useI18n();
  return (
    <div role="alert" className="notice error">
      <div>
        {error instanceof APIError && error.status === 409
          ? t("conflict")
          : errorText(error, lang)}
      </div>
      {retry && (
        <button className="text-button" onClick={retry}>
          <RefreshCw size={16} />
          {t("sync")}
        </button>
      )}
    </div>
  );
}
export function Empty({ text, action }: { text?: string; action?: ReactNode }) {
  const { t } = useI18n();
  return (
    <div className="empty">
      <Sunrise size={38} />
      <h3>{text ?? t("empty")}</h3>
      <p>{t("emptyHint")}</p>
      {action}
    </div>
  );
}
export function Status({ state }: { state: string }) {
  const { lang } = useI18n();
  return (
    <span className={`status status-${state.toLowerCase()}`}>
      <span />
      {stateName(state, lang)}
    </span>
  );
}
export function Modal({
  title,
  children,
  onClose,
  wide = false,
}: {
  title: string;
  children: ReactNode;
  onClose: () => void;
  wide?: boolean;
}) {
  const { t } = useI18n();
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  useEffect(() => {
    const element = ref.current;
    element?.showModal();
    return () => element?.close();
  }, []);
  return (
    <dialog
      ref={ref}
      aria-labelledby={titleId}
      className={wide ? "wide" : ""}
      onCancel={onClose}
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="dialog-header">
        <h2 id={titleId}>{title}</h2>
        <button
          className="icon-button"
          onClick={onClose}
          aria-label={t("close")}
        >
          <X />
        </button>
      </div>
      <div className="dialog-body">{children}</div>
    </dialog>
  );
}
export function Field({
  label,
  children,
  hint,
  controlId,
}: {
  label: string;
  children: ReactNode;
  hint?: string;
  controlId?: string;
}) {
  const generatedId = useId();
  const nativeControl =
    isValidElement<{ id?: string; "aria-describedby"?: string }>(children) &&
    typeof children.type === "string" &&
    ["input", "select", "textarea"].includes(children.type);
  const id =
    controlId ??
    (nativeControl ? (children.props.id ?? generatedId) : undefined);
  const control = nativeControl
    ? cloneElement(children, {
        id,
        ...(hint
          ? {
              "aria-describedby":
                `${children.props["aria-describedby"] ?? ""} ${generatedId}-hint`.trim(),
            }
          : {}),
      })
    : children;
  return (
    <div className="field">
      {id ? <label htmlFor={id}>{label}</label> : <span>{label}</span>}
      {control}
      {hint && <small id={`${generatedId}-hint`}>{hint}</small>}
    </div>
  );
}
export function ActionForm({
  onSubmit,
  children,
  label,
  secondary,
}: {
  onSubmit: (form: FormData) => Promise<void>;
  children: ReactNode;
  label: string;
  secondary?: ReactNode;
}) {
  const [busy, setBusy] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [error, setError] = useState<unknown>();
  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (busy) return;
    setBusy(true);
    setError(undefined);
    try {
      await onSubmit(new FormData(e.currentTarget));
      setDirty(false);
    } catch (err) {
      setError(err);
    } finally {
      setBusy(false);
    }
  }
  return (
    <form
      data-dirty={dirty}
      onChange={() => setDirty(true)}
      onSubmit={(e) => void submit(e)}
    >
      {children}
      {error !== undefined && <ErrorNotice error={error} />}
      <div className="form-actions">
        {secondary}
        <button className="button primary" disabled={busy}>
          {busy ? (
            <LoaderCircle className="spin" size={18} />
          ) : (
            <Check size={18} />
          )}{" "}
          {label}
        </button>
      </div>
    </form>
  );
}
export function Pager({
  page,
  total,
  size,
  onChange,
}: {
  page: number;
  total: number;
  size: number;
  onChange: (page: number) => void;
}) {
  const { t } = useI18n();
  return (
    <div className="pager">
      <span>
        {total} {t("records")}
      </span>
      <div>
        <button
          className="icon-button"
          disabled={page === 1}
          onClick={() => onChange(page - 1)}
          aria-label={t("previous")}
        >
          <ChevronLeft />
        </button>
        <span>
          {t("page")} {page} / {Math.max(1, Math.ceil(total / size))}
        </span>
        <button
          className="icon-button"
          disabled={page * size >= total}
          onClick={() => onChange(page + 1)}
          aria-label={t("next")}
        >
          <ChevronRight />
        </button>
      </div>
    </div>
  );
}
export function PageHeader({
  title,
  description,
  action,
}: {
  title: string;
  description?: string;
  action?: ReactNode;
}) {
  return (
    <div className="page-header">
      <div>
        <h1>{title}</h1>
        {description && <p>{description}</p>}
      </div>
      {action}
    </div>
  );
}
export function LinkButton({
  children,
  onClick,
}: {
  children: ReactNode;
  onClick: () => void;
}) {
  return (
    <button className="text-button" onClick={onClick}>
      {children}
      <ArrowRight size={16} />
    </button>
  );
}
