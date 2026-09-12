import { Eye, EyeOff, KeyRound, Lock, Sparkles, Undo2 } from "lucide-react";
import { useId, useState, type InputHTMLAttributes, type ReactNode } from "react";
import type { SettingField } from "@/api/types";
import { cn } from "@/lib/utils";
import { Button } from "../ui/button";
import { Select, SelectItem } from "../ui/select";
import type { ServiceForm } from "./useServiceForm";

export const inputClass =
  "h-10 w-full min-w-0 rounded-[var(--radius-control)] border border-border bg-surface px-3 text-[15px] text-text placeholder:text-text-muted/70 transition-colors hover:border-text-muted/50 focus:border-accent focus:outline-none disabled:cursor-not-allowed disabled:bg-surface-raised disabled:text-text-muted aria-[invalid=true]:border-danger";

interface ShellProps {
  id: string;
  label: string;
  optional?: boolean;
  help?: ReactNode;
  error?: string;
  field?: SettingField;
  configFile?: string;
  children: ReactNode;
}

export function FieldShell({ id, label, optional, help, error, field, configFile, children }: ShellProps) {
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <div className="flex items-baseline justify-between gap-3">
        <label htmlFor={id} className="text-sm font-medium">
          {label}
          {optional && <span className="ml-1.5 font-normal text-text-muted">optional</span>}
        </label>
        {field?.hint && (
          <span className="inline-flex items-center gap-1 text-xs text-success">
            <Sparkles className="size-3" /> {field.hint}
          </span>
        )}
      </div>
      {children}
      {field?.locked ? (
        <p id={`${id}-note`} className="flex items-center gap-1.5 text-xs text-text-muted">
          <Lock className="size-3 shrink-0" />
          {field.source === "env" ? (
            <>
              Set by <code className="rounded bg-surface-raised px-1 py-px text-[11px] text-text">{field.env}</code>
            </>
          ) : (
            <>Set in {configFile ? <code className="rounded bg-surface-raised px-1 py-px text-[11px] text-text">{configFile}</code> : "the config file"}</>
          )}
        </p>
      ) : error ? (
        <p id={`${id}-note`} role="alert" className="text-xs text-danger">
          {error}
        </p>
      ) : help ? (
        <p id={`${id}-note`} className="text-xs leading-relaxed text-text-muted [&_code]:rounded [&_code]:bg-surface-raised [&_code]:px-1 [&_code]:py-px [&_code]:text-[11px] [&_code]:text-text">
          {help}
        </p>
      ) : null}
    </div>
  );
}

interface TextFieldProps {
  form: ServiceForm;
  k: string;
  label: string;
  optional?: boolean;
  help?: ReactNode;
  placeholder?: string;
  type?: InputHTMLAttributes<HTMLInputElement>["type"];
  inputMode?: InputHTMLAttributes<HTMLInputElement>["inputMode"];
  min?: number;
  max?: number;
  step?: number;
  className?: string;
}

export function TextField({ form, k, label, optional, help, placeholder, type = "text", inputMode, min, max, step, className }: TextFieldProps) {
  const id = useId();
  const field = form.field(k);
  const error = form.error(k);
  const numeric = type === "number";
  return (
    <div className={className}>
      <FieldShell id={id} label={label} optional={optional} help={help} error={error} field={field} configFile={form.settings.read_only.config_file}>
        <div className="relative">
          <input
            id={id}
            type={type}
            inputMode={inputMode}
            min={min}
            max={max}
            step={step}
            value={form.value(k)}
            placeholder={placeholder}
            disabled={field?.locked}
            aria-invalid={!!error}
            aria-describedby={`${id}-note`}
            autoComplete="off"
            spellCheck={false}
            onChange={(e) => {
              const raw = e.target.value;
              form.set(k, numeric && raw !== "" && !Number.isNaN(Number(raw)) ? Number(raw) : raw);
            }}
            className={cn(inputClass, field?.locked && "pr-9")}
          />
          {field?.locked && <Lock className="pointer-events-none absolute top-1/2 right-3 size-4 -translate-y-1/2 text-text-muted" />}
        </div>
      </FieldShell>
    </div>
  );
}

export function SelectField({
  form,
  k,
  label,
  options,
  help,
  className,
}: {
  form: ServiceForm;
  k: string;
  label: string;
  options: { value: string; label: string }[];
  help?: ReactNode;
  className?: string;
}) {
  const id = useId();
  const field = form.field(k);
  const error = form.error(k);
  const value = form.value(k);
  return (
    <div className={className}>
      <FieldShell id={id} label={label} help={help} error={error} field={field} configFile={form.settings.read_only.config_file}>
        {field?.locked ? (
          <div className="relative">
            <input id={id} value={options.find((o) => o.value === value)?.label ?? value} disabled className={cn(inputClass, "pr-9")} />
            <Lock className="pointer-events-none absolute top-1/2 right-3 size-4 -translate-y-1/2 text-text-muted" />
          </div>
        ) : (
          <Select value={value} onValueChange={(v) => form.set(k, v)} label={label} className="h-10 w-full text-[15px]">
            {options.map((o) => (
              <SelectItem key={o.value} value={o.value}>
                {o.label}
              </SelectItem>
            ))}
          </Select>
        )}
      </FieldShell>
    </div>
  );
}

/** Write-only secret: never shows the saved value. */
export function SecretField({ form, k, label, optional, help, placeholder }: { form: ServiceForm; k: string; label: string; optional?: boolean; help?: ReactNode; placeholder?: string }) {
  const id = useId();
  const field = form.field(k);
  const error = form.error(k);
  const draft = form.draftOf(k);
  const [replacing, setReplacing] = useState(false);
  const [visible, setVisible] = useState(false);

  const savedSecret = field?.set && field.source !== "default";
  const removing = draft === null;
  const editing = !field?.locked && (!savedSecret || replacing) && !removing;

  return (
    <FieldShell id={id} label={label} optional={optional} help={help} error={error} field={field} configFile={form.settings.read_only.config_file}>
      {field?.locked ? (
        <div className="relative">
          <input id={id} value="••••••••••••" disabled className={cn(inputClass, "pr-9 tracking-widest")} aria-label={`${label}, set outside the UI`} />
          <Lock className="pointer-events-none absolute top-1/2 right-3 size-4 -translate-y-1/2 text-text-muted" />
        </div>
      ) : removing ? (
        <div className="flex h-10 items-center gap-2 rounded-[var(--radius-control)] border border-dashed border-danger/50 bg-danger/5 pr-1 pl-3 text-sm">
          <span id={id} className="flex-1 text-danger">
            Removed when you save
          </span>
          <Button size="sm" variant="ghost" onClick={() => form.revert(k)}>
            <Undo2 /> Undo
          </Button>
        </div>
      ) : editing ? (
        <div className="flex gap-2">
          <div className="relative min-w-0 flex-1">
            <input
              id={id}
              type={visible ? "text" : "password"}
              value={typeof draft === "string" ? draft : ""}
              onChange={(e) => form.set(k, e.target.value)}
              placeholder={placeholder ?? (replacing ? "Paste the new value" : "Paste it here")}
              autoComplete="new-password"
              spellCheck={false}
              aria-invalid={!!error}
              aria-describedby={`${id}-note`}
              className={cn(inputClass, "pr-10 font-mono text-sm placeholder:font-sans placeholder:text-[15px]")}
              autoFocus={replacing}
            />
            <button
              type="button"
              onClick={() => setVisible((v) => !v)}
              aria-label={visible ? `Hide ${label}` : `Show ${label}`}
              className="absolute top-1/2 right-1 grid size-8 -translate-y-1/2 place-items-center rounded-md text-text-muted hover:text-text"
            >
              {visible ? <EyeOff className="size-4" /> : <Eye className="size-4" />}
            </button>
          </div>
          {replacing && (
            <Button
              variant="ghost"
              onClick={() => {
                setReplacing(false);
                form.revert(k);
              }}
            >
              Cancel
            </Button>
          )}
        </div>
      ) : (
        <div className="flex h-10 items-center gap-1 rounded-[var(--radius-control)] border border-border bg-surface-raised pr-1 pl-3">
          <KeyRound className="size-4 shrink-0 text-success" />
          <span id={id} className="ml-2 flex-1 truncate text-sm">
            <span className="tracking-[0.2em] text-text-muted" aria-hidden>
              ••••••••
            </span>{" "}
            saved
          </span>
          <Button size="sm" variant="ghost" onClick={() => setReplacing(true)} aria-label={`Replace ${label}`}>
            Replace
          </Button>
          <Button size="sm" variant="danger" onClick={() => form.set(k, null)} aria-label={`Clear ${label}`}>
            Clear
          </Button>
        </div>
      )}
    </FieldShell>
  );
}
