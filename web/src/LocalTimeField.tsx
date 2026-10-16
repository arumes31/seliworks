import { useEffect, useRef, useState } from "react";
import { localInput, localTimeCandidates } from "./api";
import { useI18n } from "./i18n";
import { Field } from "./ui";

export function LocalTimeField({
  name,
  label,
  instant,
  required = true,
  onInstantChange,
}: {
  name: string;
  label: string;
  instant?: string;
  required?: boolean;
  onInstantChange?: (instant: string) => void;
}) {
  const { t } = useI18n();
  const initial = instant ? localInput(instant, true) : "";
  const [value, setValue] = useState(initial);
  const selectedFor = (value: string, source?: string) =>
    localTimeCandidates(value).find(
      (candidate) =>
        source &&
        candidate.slice(0, 19) === new Date(source).toISOString().slice(0, 19),
    ) || "";
  const [choice, setChoice] = useState(() => selectedFor(initial, instant));
  const input = useRef<HTMLInputElement>(null);
  const emitted = useRef(instant);
  const candidates = localTimeCandidates(value);
  useEffect(() => {
    input.current?.setCustomValidity(
      value && !candidates.length ? t("nonexistentLocal") : "",
    );
  }, [value, candidates.length, t]);
  useEffect(() => {
    if (onInstantChange && instant && emitted.current !== instant) {
      emitted.current = instant;
      const local = localInput(instant, true);
      setValue(local);
      setChoice(selectedFor(local, instant));
    }
  }, [instant, onInstantChange]);
  function emit(candidate: string, local: string) {
    const exact =
      instant &&
      local === localInput(instant, true) &&
      candidate.slice(0, 19) === new Date(instant).toISOString().slice(0, 19)
        ? instant
        : candidate;
    emitted.current = exact;
    onInstantChange?.(exact);
  }
  return (
    <>
      <Field label={label}>
        <input
          ref={input}
          type="datetime-local"
          step="1"
          name={name}
          required={required}
          value={value}
          onChange={(event) => {
            const next = event.target.value;
            setValue(next);
            const options = localTimeCandidates(next);
            setChoice(options.length === 1 ? options[0]! : "");
            if (options.length === 1) emit(options[0]!, next);
          }}
        />
      </Field>
      {candidates.length > 1 && (
        <Field
          label={`${label} · ${t("offsetChoice")}`}
          hint={t("ambiguousLocal")}
        >
          <select
            name={name + "_offset"}
            value={choice}
            required
            onChange={(event) => {
              setChoice(event.target.value);
              if (event.target.value) emit(event.target.value, value);
            }}
          >
            <option value="">{t("select")}</option>
            {candidates.map((candidate) => {
              const minutes = -new Date(candidate).getTimezoneOffset();
              const offset = `UTC${minutes >= 0 ? "+" : "-"}${String(Math.floor(Math.abs(minutes) / 60)).padStart(2, "0")}:${String(Math.abs(minutes) % 60).padStart(2, "0")}`;
              return (
                <option value={candidate} key={candidate}>
                  {offset}
                </option>
              );
            })}
          </select>
        </Field>
      )}
    </>
  );
}
