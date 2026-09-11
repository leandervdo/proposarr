import { Loader2 } from "lucide-react";
import { useCallback, useEffect, useState, type ReactNode } from "react";
import { toast } from "sonner";
import { useSettings } from "@/api/queries";
import type { Kind, Settings } from "@/api/types";
import { ErrorNote } from "@/components/EmptyState";
import { PageHeader } from "@/components/PageHeader";
import { SelectField, TextField } from "@/components/settings/fields";
import { UnsavedGuard } from "@/components/settings/UnsavedGuard";
import { useServiceForm, type ServiceForm } from "@/components/settings/useServiceForm";
import { Button } from "@/components/ui/button";
import { Select, SelectItem } from "@/components/ui/select";
import { cn } from "@/lib/utils";

const RUN_FIELDS = ["model", "effort", "picks", "candidates", "free_picks", "seeds", "top_titles"] as const;
const GENERAL_KEYS = ["history_days", "snapshot_ttl", "tmdb.region", "claude.timeout", "claude.max_budget_usd"] as const;

const EFFORT = [
  { value: "low", label: "Low, fastest" },
  { value: "medium", label: "Medium" },
  { value: "high", label: "High" },
  { value: "xhigh", label: "Extra high" },
  { value: "max", label: "Max, slowest" },
];

const REGIONS: [string, string][] = [
  ["US", "United States"], ["GB", "United Kingdom"], ["CA", "Canada"], ["AU", "Australia"], ["NL", "Netherlands"],
  ["BE", "Belgium"], ["DE", "Germany"], ["FR", "France"], ["ES", "Spain"], ["IT", "Italy"], ["SE", "Sweden"],
  ["NO", "Norway"], ["DK", "Denmark"], ["IE", "Ireland"], ["BR", "Brazil"], ["IN", "India"], ["JP", "Japan"],
];

export function SettingsPage() {
  const settings = useSettings();
  const [dirty, setDirty] = useState<Record<string, boolean>>({});
  const onDirtyChange = useCallback((id: string, d: boolean) => setDirty((prev) => (prev[id] === d ? prev : { ...prev, [id]: d })), []);

  return (
    <>
      <PageHeader title="Settings">How runs behave. Addresses and keys for your services are on the Connections page.</PageHeader>
      {settings.isPending ? (
        <div className="grid gap-5 lg:grid-cols-2" aria-busy>
          {[0, 1].map((i) => (
            <div key={i} className="h-[30rem] animate-pulse rounded-[var(--radius-panel)] bg-surface" />
          ))}
        </div>
      ) : settings.isError ? (
        <ErrorNote title="Could not load settings" message={settings.error.message} onRetry={() => void settings.refetch()} />
      ) : (
        <div className="flex flex-col gap-12">
          <section>
            <h2 className="mb-4 font-display text-[28px] leading-none font-bold tracking-tight">Runs</h2>
            <div className="grid items-start gap-5 lg:grid-cols-2">
              {(["movies", "series"] as const).map((kind) => (
                <Panel
                  key={kind}
                  id={kind}
                  title={kind === "movies" ? "Movies" : "Series"}
                  keys={RUN_FIELDS.map((f) => `${kind}.${f}`)}
                  settings={settings.data}
                  onDirtyChange={onDirtyChange}
                >
                  {(form) => <RunFields kind={kind} form={form} />}
                </Panel>
              ))}
            </div>
          </section>

          <section className="grid items-start gap-12 lg:grid-cols-2 lg:gap-5">
            <div>
              <h2 className="mb-4 font-display text-[28px] leading-none font-bold tracking-tight">General</h2>
              <Panel id="general" title="History, caching and limits" keys={[...GENERAL_KEYS]} settings={settings.data} onDirtyChange={onDirtyChange}>
                {(form) => <GeneralFields form={form} />}
              </Panel>
            </div>
            <div>
              <h2 className="mb-4 font-display text-[28px] leading-none font-bold tracking-tight">Set outside the app</h2>
              <ReadOnly settings={settings.data} />
            </div>
          </section>
        </div>
      )}
      <UnsavedGuard when={Object.values(dirty).some(Boolean)} />
    </>
  );
}

function Panel({
  id,
  title,
  keys,
  settings,
  onDirtyChange,
  children,
}: {
  id: string;
  title: string;
  keys: string[];
  settings: Settings;
  onDirtyChange: (id: string, dirty: boolean) => void;
  children: (form: ServiceForm) => ReactNode;
}) {
  const form = useServiceForm(settings, keys);
  useEffect(() => onDirtyChange(id, form.dirty), [id, form.dirty, onDirtyChange]);

  const save = async () => {
    if (await form.save()) toast.success(`${title} saved`);
  };

  return (
    <section className={cn("flex flex-col rounded-[var(--radius-panel)] border bg-surface", form.dirty ? "border-accent/45" : "border-border")}>
      <h3 className="px-5 pt-5 text-[17px] font-semibold">{title}</h3>
      <div className="px-5 py-5">{children(form)}</div>
      {form.saveError && (
        <p role="alert" className="px-5 pb-4 text-sm text-danger">
          {form.saveError}
        </p>
      )}
      <footer className="flex flex-wrap items-center gap-2 rounded-b-[var(--radius-panel)] border-t border-border bg-surface-raised px-5 py-3">
        {form.dirty && (
          <span className="flex items-center gap-1.5 text-xs font-medium text-accent">
            <span className="size-1.5 rounded-full bg-accent" /> Unsaved changes
          </span>
        )}
        <div className="ml-auto flex gap-2">
          {form.dirty && (
            <Button size="sm" variant="ghost" onClick={form.reset}>
              Discard
            </Button>
          )}
          <Button size="sm" variant={form.dirty ? "primary" : "secondary"} onClick={() => void save()} disabled={!form.dirty || form.saving}>
            {form.saving && <Loader2 className="animate-spin" />} Save
          </Button>
        </div>
      </footer>
    </section>
  );
}

function RunFields({ kind, form }: { kind: Kind; form: ServiceForm }) {
  const k = (f: string) => `${kind}.${f}`;
  return (
    <div className="grid gap-4 sm:grid-cols-2">
      <TextField form={form} k={k("model")} label="Model" placeholder="claude-sonnet-5" help="claude-sonnet-5 is a good balance. claude-opus-5 thinks harder and costs more." />
      <SelectField form={form} k={k("effort")} label="Effort" options={EFFORT} help="Higher effort takes longer and uses more tokens." />
      <TextField form={form} k={k("picks")} label="Picks per run" type="number" inputMode="numeric" min={1} max={50} help="1 to 50." />
      <TextField form={form} k={k("candidates")} label="Candidates" type="number" inputMode="numeric" min={10} max={500} help="Titles from TMDB that Claude chooses from, 10 to 500." />
      <TextField form={form} k={k("free_picks")} label="Picks from outside the list" type="number" inputMode="numeric" min={0} max={10} help="Titles Claude may suggest on its own, 0 to 10." />
      <TextField form={form} k={k("seeds")} label="Seed titles" type="number" inputMode="numeric" min={1} max={50} help="Your top titles used to look up candidates, 1 to 50." />
      <TextField form={form} k={k("top_titles")} label="Profile size" type="number" inputMode="numeric" min={5} max={1000} help="How many of your titles Claude sees, watched titles first. Libraries up to this size are sent in full. 5 to 1000." className="sm:col-span-2" />
    </div>
  );
}

function GeneralFields({ form }: { form: ServiceForm }) {
  return (
    <div className="grid gap-4 sm:grid-cols-2">
      <TextField form={form} k="history_days" label="Watch history window" type="number" inputMode="numeric" min={1} max={3650} help="Days of Plex or Jellyfin history that count." />
      <TextField form={form} k="snapshot_ttl" label="Library cache" placeholder="6h" help="How long a library read is reused, like 30m or 6h." />
      <RegionField form={form} />
      <TextField form={form} k="claude.timeout" label="Run time limit" placeholder="10m" help="Longest a run may take, like 10m." />
      <TextField
        form={form}
        k="claude.max_budget_usd"
        label="Spend limit per run, in USD"
        type="number"
        inputMode="decimal"
        min={0}
        step={0.05}
        help="Stops a run that would cost more. 0 means no limit."
        className="sm:col-span-2"
      />
    </div>
  );
}

function RegionField({ form }: { form: ServiceForm }) {
  const field = form.field("tmdb.region");
  const value = form.value("tmdb.region").toUpperCase();
  const known = REGIONS.some(([code]) => code === value);
  const [other, setOther] = useState(!known && value !== "");

  if (field?.locked || other) {
    return (
      <div className="flex flex-col gap-1.5">
        <TextField form={form} k="tmdb.region" label="Streaming region" placeholder="Two letters, like NL" help={field?.locked ? undefined : "ISO country code, used for where titles stream."} />
        {!field?.locked && (
          <button type="button" className="self-start text-xs text-accent underline underline-offset-2" onClick={() => setOther(false)}>
            Pick from the list
          </button>
        )}
      </div>
    );
  }
  return (
    <div className="flex min-w-0 flex-col gap-1.5">
      <span className="text-sm font-medium">Streaming region</span>
      <Select
        value={known ? value : ""}
        onValueChange={(v) => (v === "other" ? setOther(true) : form.set("tmdb.region", v))}
        label="Streaming region"
        placeholder="Choose a country"
        className="h-10 w-full text-[15px]"
      >
        {REGIONS.map(([code, name]) => (
          <SelectItem key={code} value={code}>
            {name}
          </SelectItem>
        ))}
        <SelectItem value="other">Another country…</SelectItem>
      </Select>
      {form.error("tmdb.region") ? (
        <p role="alert" className="text-xs text-danger">
          {form.error("tmdb.region")}
        </p>
      ) : (
        <p className="text-xs text-text-muted">Used to show where titles stream.</p>
      )}
    </div>
  );
}

function ReadOnly({ settings }: { settings: Settings }) {
  const ro = settings.read_only;
  const rows: [string, ReactNode, string][] = [
    ["Web address", ro.listen, "PROPOSARR_LISTEN"],
    ["Data folder", ro.data_dir, "PROPOSARR_DATA_DIR"],
    ["Claude Code binary", ro.claude_bin, "PROPOSARR_CLAUDE_BIN"],
    ["Web login", ro.web_auth ? "On" : "Off, anyone who can reach this page can use it", "PROPOSARR_WEB_USERNAME and PROPOSARR_WEB_PASSWORD"],
    ["Config file", ro.config_file ?? "None", "PROPOSARR_CONFIG"],
  ];
  return (
    <div className="rounded-[var(--radius-panel)] border border-border bg-surface">
      <dl className="divide-y divide-border">
        {rows.map(([label, value, env]) => (
          <div key={label} className="grid gap-1 px-5 py-4 sm:grid-cols-[10rem_1fr] sm:gap-4">
            <dt className="text-sm text-text-muted">{label}</dt>
            <dd className="min-w-0">
              <p className={cn("break-words text-[15px]", label === "Web login" && !ro.web_auth && "text-warning")}>{value}</p>
              <p className="mt-0.5 text-xs break-words text-text-muted">
                Change with <code className="rounded bg-surface-raised px-1 py-px text-[11px] text-text">{env}</code>
              </p>
            </dd>
          </div>
        ))}
      </dl>
      <p className="border-t border-border px-5 py-4 text-sm text-text-muted">
        These are read when Proposarr starts. Set them as environment variables on the container, or in the config file, and restart.
      </p>
    </div>
  );
}
