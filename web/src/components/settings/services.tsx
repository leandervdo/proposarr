import { AlertCircle, Bot, Check, CircleDashed, Database, Film, Loader2, PlugZap, Server, Sparkles, Tv, type LucideIcon } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { toast } from "sonner";
import type { CheckResult, Service, Settings, TestResult } from "@/api/types";
import { cn } from "@/lib/utils";
import { Button } from "../ui/button";
import { Segmented } from "../ui/segmented";
import { ProfileRankingField } from "./ProfileRankingField";
import { SecretField, SelectField, TextField } from "./fields";
import { useServiceForm, type ServiceForm } from "./useServiceForm";

export const SERVICE_KEYS: Record<Service, readonly string[]> = {
  radarr: ["radarr.url", "radarr.api_key", "radarr.root_folder", "radarr.minimum_availability", "radarr.profile_order"],
  sonarr: ["sonarr.url", "sonarr.api_key", "sonarr.root_folder"],
  plex: ["plex.url", "plex.token"],
  jellyfin: ["jellyfin.url", "jellyfin.api_key", "jellyfin.user_id"],
  tmdb: ["tmdb.api_key"],
  claude: ["claude.oauth_token", "claude.api_key"],
};

export const SERVICE_META: Record<Service, { name: string; icon: LucideIcon; description: string }> = {
  radarr: { name: "Radarr", icon: Film, description: "Your movie library. Accepted movie picks are added here." },
  sonarr: { name: "Sonarr", icon: Tv, description: "Your series library. Accepted series picks are added here." },
  plex: { name: "Plex", icon: Server, description: "Watch history, the strongest signal for what you like." },
  jellyfin: { name: "Jellyfin", icon: Server, description: "Watch history, if you use Jellyfin instead of Plex." },
  tmdb: { name: "TMDB", icon: Database, description: "Titles, posters and where things stream. Needs a free API key." },
  claude: { name: "Claude", icon: Bot, description: "Ranks the candidates against your taste and explains each pick." },
};

const AVAILABILITY = [
  { value: "announced", label: "When announced" },
  { value: "inCinemas", label: "When in cinemas" },
  { value: "released", label: "When released" },
];

/** The inputs for one service. Used by the Connections page and first-run setup (which ranks profiles in a step of its own). */
export function ServiceFields({ service, form, ranking = true }: { service: Service; form: ServiceForm; ranking?: boolean }) {
  switch (service) {
    case "radarr":
    case "sonarr": {
      const name = SERVICE_META[service].name;
      const port = service === "radarr" ? 7878 : 8989;
      return (
        <div className="grid gap-4 sm:grid-cols-2">
          <TextField form={form} k={`${service}.url`} label="Address" type="url" inputMode="url" placeholder={`http://192.168.1.10:${port}`} className="sm:col-span-2" />
          <div className="sm:col-span-2">
            <SecretField
              form={form}
              k={`${service}.api_key`}
              label="API key"
              optional
              help={`Leave empty to read it automatically from ${name} (works when ${name} does not require a login on your network). Otherwise find it in ${name} under Settings, General.`}
            />
          </div>
          <TextField form={form} k={`${service}.root_folder`} label="Root folder" optional placeholder={service === "radarr" ? "/media/movies" : "/media/series"} help="Leave empty to be asked when there is more than one." />
          {service === "radarr" && (
            <>
              <SelectField form={form} k="radarr.minimum_availability" label="Search for movies" options={AVAILABILITY} />
              {ranking && <ProfileRankingField form={form} className="sm:col-span-2" />}
            </>
          )}
        </div>
      );
    }
    case "plex":
      return (
        <div className="grid gap-4">
          <TextField form={form} k="plex.url" label="Address" type="url" inputMode="url" placeholder="http://192.168.1.10:32400" />
          <SecretField
            form={form}
            k="plex.token"
            label="Token"
            help={<>In Plex Web, open any title, choose Get Info, then View XML, and copy the <code>X-Plex-Token</code> value from the address bar.</>}
          />
        </div>
      );
    case "jellyfin":
      return (
        <div className="grid gap-4 sm:grid-cols-2">
          <TextField form={form} k="jellyfin.url" label="Address" type="url" inputMode="url" placeholder="http://192.168.1.10:8096" className="sm:col-span-2" />
          <SecretField form={form} k="jellyfin.api_key" label="API key" help="Create one in Jellyfin under Dashboard, API Keys." />
          <TextField form={form} k="jellyfin.user_id" label="User ID" optional help="Leave empty to combine every user's history." />
        </div>
      );
    case "tmdb":
      return (
        <SecretField
          form={form}
          k="tmdb.api_key"
          label="API key"
          help={
            <>
              Free with a TMDB account: open{" "}
              <a href="https://www.themoviedb.org/settings/api" target="_blank" rel="noreferrer" className="text-accent underline underline-offset-2">
                themoviedb.org API settings
              </a>{" "}
              and copy the API key or the read access token. Either works.
            </>
          }
        />
      );
    case "claude":
      return <ClaudeFields form={form} />;
  }
}

function ClaudeFields({ form }: { form: ServiceForm }) {
  const token = form.field("claude.oauth_token");
  const key = form.field("claude.api_key");
  const initial = key?.set && key.source !== "default" && !(token?.set && token.source !== "default") ? "api" : "subscription";
  const [mode, setMode] = useState<"subscription" | "api">(initial);
  const locked = token?.locked || key?.locked;

  const switchTo = (next: "subscription" | "api") => {
    const other = next === "api" ? "claude.oauth_token" : "claude.api_key";
    const otherField = form.field(other);
    // Only one credential may be set: drop the other one in the draft.
    if (typeof form.draftOf(other) === "string") form.revert(other);
    if (otherField?.set && otherField.source === "ui") form.set(other, null);
    setMode(next);
  };

  return (
    <div className="grid gap-4">
      <Segmented
        label="How Proposarr pays for Claude"
        value={mode}
        onChange={(v) => !locked && switchTo(v)}
        options={[
          { value: "subscription", label: "Claude subscription" },
          { value: "api", label: "Anthropic API key" },
        ]}
        className="self-start"
      />
      {mode === "subscription" ? (
        <SecretField
          form={form}
          k="claude.oauth_token"
          label="Subscription token"
          placeholder="sk-ant-oat01-…"
          help={<>Run <code>claude setup-token</code> on any computer with Claude Code, sign in, and paste the token it prints.</>}
        />
      ) : (
        <SecretField form={form} k="claude.api_key" label="API key" placeholder="sk-ant-api03-…" help="Create a key in the Anthropic Console. Runs are billed to that account." />
      )}
      <p className="text-xs text-text-muted">Test makes one short model call.</p>
    </div>
  );
}

export function TestNote({ result, testing, service }: { result: TestResult | null; testing: boolean; service: Service }) {
  if (testing) {
    return (
      <p className="flex items-center gap-2 text-sm text-text-muted" aria-live="polite">
        <Loader2 className="size-4 animate-spin" /> Testing {SERVICE_META[service].name}…
      </p>
    );
  }
  if (!result) return null;
  const ok = result.status === "ok";
  return (
    <div
      role={ok ? "status" : "alert"}
      className={cn("flex items-start gap-2 rounded-[var(--radius-control)] border px-3 py-2.5 text-sm", ok ? "border-success/35 bg-success/8" : "border-danger/35 bg-danger/8")}
    >
      {ok ? <Check className="mt-0.5 size-4 shrink-0 text-success" /> : <AlertCircle className="mt-0.5 size-4 shrink-0 text-danger" />}
      <div className="min-w-0">
        <p className="font-medium">{ok ? "Connected" : "Not working"}</p>
        {result.detail && <p className="mt-0.5 break-words text-text-muted">{result.detail}</p>}
        {ok && result.discovered_api_key && (
          <p className="mt-1 flex items-center gap-1.5 text-success">
            <Sparkles className="size-3.5" /> Found the API key automatically, no need to paste it.
          </p>
        )}
      </div>
    </div>
  );
}

type LiveStatus = "ok" | "fail" | "skip" | undefined;

function StatusPill({ status }: { status: LiveStatus }) {
  if (!status) return null;
  const map = {
    ok: { label: "Connected", icon: Check, className: "border-success/40 text-success" },
    fail: { label: "Not working", icon: AlertCircle, className: "border-danger/40 text-danger" },
    skip: { label: "Not set up", icon: CircleDashed, className: "border-border text-text-muted" },
  }[status];
  const Icon = map.icon;
  return (
    <span className={cn("inline-flex h-7 shrink-0 items-center gap-1.5 rounded-full border px-2.5 text-xs font-medium", map.className)}>
      <Icon className="size-3.5" /> {map.label}
    </span>
  );
}

export function liveStatusFor(service: Service, checks: CheckResult[] | undefined): LiveStatus {
  if (!checks) return undefined;
  if (service === "claude") {
    const rows = checks.filter((c) => c.name === "claude" || c.name === "Claude auth");
    if (rows.length === 0) return undefined;
    return rows.some((r) => r.status === "fail") ? "fail" : "ok";
  }
  return checks.find((c) => c.name === SERVICE_META[service].name)?.status;
}

interface ServiceCardProps {
  service: Service;
  settings: Settings;
  checked?: LiveStatus;
  onDirtyChange: (service: Service, dirty: boolean) => void;
  aside?: ReactNode;
}

export function ServiceCard({ service, settings, checked, onDirtyChange }: ServiceCardProps) {
  const form = useServiceForm(settings, SERVICE_KEYS[service]);
  const meta = SERVICE_META[service];
  const Icon = meta.icon;

  useEffect(() => onDirtyChange(service, form.dirty), [service, form.dirty, onDirtyChange]);

  const save = async () => {
    const next = await form.save();
    if (next) toast.success(`${meta.name} settings saved`);
  };

  const status: LiveStatus = form.testResult?.status ?? checked;

  return (
    <form
      aria-labelledby={`${service}-title`}
      onSubmit={(e) => {
        e.preventDefault();
        if (form.dirty && !form.saving) void save();
      }}
      className={cn("flex flex-col rounded-[var(--radius-panel)] border bg-surface", form.dirty ? "border-accent/45" : "border-border")}
    >
      <header className="flex items-start gap-3 px-5 pt-5">
        <span className="grid size-10 shrink-0 place-items-center rounded-[10px] border border-border bg-surface-raised text-text-muted">
          <Icon className="size-5" strokeWidth={1.75} />
        </span>
        <div className="min-w-0 flex-1">
          <h3 id={`${service}-title`} className="text-[17px] leading-tight font-semibold">
            {meta.name}
          </h3>
          <p className="mt-0.5 text-sm text-text-muted">{meta.description}</p>
        </div>
        <StatusPill status={status} />
      </header>

      <div className="flex flex-1 flex-col gap-4 px-5 py-5">
        <ServiceFields service={service} form={form} />
        <TestNote result={form.testResult} testing={form.testing} service={service} />
        {form.saveError && (
          <p role="alert" className="text-sm text-danger">
            {form.saveError}
          </p>
        )}
      </div>

      <footer className="flex flex-wrap items-center gap-2 rounded-b-[var(--radius-panel)] border-t border-border bg-surface-raised px-5 py-3">
        {form.dirty ? (
          <span className="flex items-center gap-1.5 text-xs font-medium text-accent">
            <span className="size-1.5 rounded-full bg-accent" /> Unsaved changes
          </span>
        ) : null}
        <div className="ml-auto flex gap-2">
          {form.dirty && (
            <Button size="sm" variant="ghost" onClick={form.reset}>
              Discard
            </Button>
          )}
          <Button size="sm" onClick={() => void form.test(service)} disabled={form.testing}>
            {form.testing ? <Loader2 className="animate-spin" /> : <PlugZap />} Test
          </Button>
          <Button size="sm" type="submit" variant={form.dirty ? "primary" : "secondary"} disabled={!form.dirty || form.saving}>
            {form.saving ? <Loader2 className="animate-spin" /> : null} Save
          </Button>
        </div>
      </footer>
    </form>
  );
}
