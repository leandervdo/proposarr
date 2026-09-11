import { Loader2, PlugZap } from "lucide-react";
import { useCallback, useState, type ReactNode } from "react";
import { useChecks, useSettings } from "@/api/queries";
import type { Service } from "@/api/types";
import { ErrorNote } from "@/components/EmptyState";
import { PageHeader } from "@/components/PageHeader";
import { liveStatusFor, ServiceCard } from "@/components/settings/services";
import { UnsavedGuard } from "@/components/settings/UnsavedGuard";
import { Button } from "@/components/ui/button";

export function ConnectionsPage() {
  const settings = useSettings();
  const checks = useChecks();
  const [dirty, setDirty] = useState<Partial<Record<Service, boolean>>>({});
  const onDirtyChange = useCallback((service: Service, d: boolean) => setDirty((prev) => (prev[service] === d ? prev : { ...prev, [service]: d })), []);
  const anyDirty = Object.values(dirty).some(Boolean);

  return (
    <>
      <PageHeader
        title="Connections"
        actions={
          <Button onClick={() => checks.mutate()} disabled={checks.isPending}>
            {checks.isPending ? <Loader2 className="animate-spin" /> : <PlugZap />}
            {checks.isPending ? "Testing" : "Test all"}
          </Button>
        }
      >
        Where Proposarr finds your library, your watch history, title data and Claude. Saved changes apply right away.
      </PageHeader>

      {checks.isError && <ErrorNote title="Could not test the connections" message={checks.error.message} />}

      {settings.isPending ? (
        <div className="grid gap-5 lg:grid-cols-2" aria-busy>
          {[0, 1, 2, 3].map((i) => (
            <div key={i} className="h-80 animate-pulse rounded-[var(--radius-panel)] bg-surface" />
          ))}
        </div>
      ) : settings.isError ? (
        <ErrorNote title="Could not load settings" message={settings.error.message} onRetry={() => void settings.refetch()} />
      ) : (
        <div className="flex flex-col gap-12">
          <Group title="Library" description="Connect Radarr, Sonarr or both. Picks you accept are added there.">
            <div className="grid items-start gap-5 lg:grid-cols-2">
              {(["radarr", "sonarr"] as const).map((s) => (
                <ServiceCard key={s} service={s} settings={settings.data} checked={liveStatusFor(s, checks.data)} onDirtyChange={onDirtyChange} />
              ))}
            </div>
          </Group>
          <Group title="Watch history" optional description="What you actually watch weighs more than what you own. Connect the server you use.">
            <div className="grid items-start gap-5 lg:grid-cols-2">
              {(["plex", "jellyfin"] as const).map((s) => (
                <ServiceCard key={s} service={s} settings={settings.data} checked={liveStatusFor(s, checks.data)} onDirtyChange={onDirtyChange} />
              ))}
            </div>
          </Group>
          <div className="grid items-start gap-12 lg:grid-cols-2 lg:gap-5">
            <Group title="Metadata">
              <ServiceCard service="tmdb" settings={settings.data} checked={liveStatusFor("tmdb", checks.data)} onDirtyChange={onDirtyChange} />
            </Group>
            <Group title="Model">
              <ServiceCard service="claude" settings={settings.data} checked={liveStatusFor("claude", checks.data)} onDirtyChange={onDirtyChange} />
            </Group>
          </div>
        </div>
      )}

      <UnsavedGuard when={anyDirty} />
    </>
  );
}

function Group({ title, description, optional, children }: { title: string; description?: string; optional?: boolean; children: ReactNode }) {
  return (
    <section>
      <div className="mb-4">
        <h2 className="font-display text-[28px] leading-none font-bold tracking-tight">
          {title}
          {optional && <span className="ml-2 align-middle font-sans text-sm font-normal tracking-normal text-text-muted">optional</span>}
        </h2>
        {description && <p className="mt-2 max-w-[62ch] text-[15px] text-text-muted">{description}</p>}
      </div>
      {children}
    </section>
  );
}
