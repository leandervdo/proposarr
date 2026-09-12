import { ArrowDown, ArrowUp } from "lucide-react";
import { useEffect, useId } from "react";
import { useAppOptions } from "@/api/queries";
import type { QualityProfile } from "@/api/types";
import { Button } from "../ui/button";
import { FieldShell } from "./fields";
import type { ServiceForm } from "./useServiceForm";

const KEY = "radarr.profile_order";

/** Profile ids a saved ranking names, best first. Entries Radarr no longer has are dropped. */
function rankedIds(value: string, profiles: QualityProfile[]): number[] {
  const ids: number[] = [];
  for (const entry of value.split(",").map((s) => s.trim().toLowerCase()).filter(Boolean)) {
    const profile = profiles.find((p) => String(p.id) === entry || p.name.toLowerCase() === entry);
    if (profile && !ids.includes(profile.id)) ids.push(profile.id);
  }
  return ids;
}

/** Every Radarr quality profile, best first. Saved as all their ids, comma separated. */
export function ProfileRankingField({ form, className }: { form: ServiceForm; className?: string }) {
  const id = useId();
  const field = form.field(KEY);
  const locked = !!field?.locked;
  const enabled = !!form.field("radarr.url")?.set;
  const options = useAppOptions("radarr", enabled);
  const { refetch } = options;
  // Profiles change in Radarr; sync them whenever the ranking is shown.
  useEffect(() => {
    if (enabled) void refetch();
  }, [enabled, refetch]);

  const profiles = options.data?.quality_profiles ?? [];
  const ranked = form.value(KEY).trim() !== "";
  const ids = rankedIds(form.value(KEY), profiles);
  const rows = [...ids, ...profiles.map((p) => p.id).filter((pid) => !ids.includes(pid))];
  const unranked = rows.length - ids.length;

  const setOrder = (order: number[]) => form.set(KEY, order.join(","));
  const move = (i: number, by: -1 | 1) => {
    const next = [...rows];
    [next[i], next[i + by]] = [next[i + by]!, next[i]!];
    setOrder(next);
  };

  return (
    <div className={className}>
      <FieldShell
        id={id}
        label="Quality ranking"
        help="When nothing fits a movie's profile and you chose to switch, Proposarr tries the profiles ranked below it, top to bottom. It never switches to a higher one."
        error={form.error(KEY)}
        field={field}
        configFile={form.settings.read_only.config_file}
      >
        {profiles.length === 0 ? (
          <p id={id} className="text-sm text-text-muted">
            {options.isPending && options.fetchStatus !== "idle"
              ? "Loading Radarr's quality profiles…"
              : options.data
                ? "Radarr has no quality profiles yet."
                : "Connect and save Radarr to rank its quality profiles"}
          </p>
        ) : (
          <div className="flex flex-col gap-2">
            {!locked && (!ranked || unranked > 0) && (
              <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2 rounded-[var(--radius-control)] border border-dashed border-border py-2 pr-2 pl-3 text-sm text-text-muted">
                <span>
                  {!ranked ? "Not ranked yet. Put the best profile on top." : unranked === 1 ? "One new profile isn't ranked yet." : `${unranked} new profiles aren't ranked yet.`}
                </span>
                <Button size="sm" onClick={() => setOrder(rows)}>
                  Use this order
                </Button>
              </div>
            )}
            <ol id={id} aria-describedby={`${id}-note`} className="divide-y divide-border overflow-hidden rounded-[var(--radius-control)] border border-border bg-surface">
              {rows.map((pid, i) => {
                const profile = profiles.find((p) => p.id === pid)!;
                return (
                  <li key={pid} className="flex items-center gap-2 py-1 pr-1 pl-3">
                    <span className="nums w-5 shrink-0 text-sm font-semibold text-text-muted">{i + 1}</span>
                    <span className="min-w-0 flex-1 truncate text-sm">{profile.name}</span>
                    {i === 0 && <span className="shrink-0 rounded-full bg-accent-soft px-2 py-0.5 text-[11px] font-medium text-accent">best</span>}
                    {ranked && !ids.includes(pid) && (
                      <span className="shrink-0 rounded-full border border-border px-2 py-0.5 text-[11px] font-medium text-text-muted">New</span>
                    )}
                    <Button size="iconSm" variant="ghost" disabled={locked || i === 0} onClick={() => move(i, -1)} aria-label={`Move ${profile.name} up`}>
                      <ArrowUp />
                    </Button>
                    <Button size="iconSm" variant="ghost" disabled={locked || i === rows.length - 1} onClick={() => move(i, 1)} aria-label={`Move ${profile.name} down`}>
                      <ArrowDown />
                    </Button>
                  </li>
                );
              })}
            </ol>
          </div>
        )}
      </FieldShell>
    </div>
  );
}
