import { AlertCircle, Clock, HardDrive, Loader2 } from "lucide-react";
import { Fragment, useEffect, useRef, useState, type ReactNode } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { ApiError } from "@/api/client";
import { useAppOptions, useConfig, usePick, useRequestPick, useSwitchProfile } from "@/api/queries";
import type { IfNothingFits, Pick, ReleaseCheck } from "@/api/types";
import { fallbackTargets } from "@/lib/fallback";
import { appFor, appName, fileSize, gigabytes } from "@/lib/format";
import { announceRelease, awaitRelease, isAwaitingRelease, takeAwaitedRelease, useReleaseShown } from "@/lib/releases";
import { TitleLinks } from "./PickMeta";
import { Poster } from "./Poster";
import { Button } from "./ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle } from "./ui/dialog";
import { RadioCard, RadioGroup } from "./ui/radio-group";
import { Select, SelectItem } from "./ui/select";

export function AcceptDialog({ pick, onClose }: { pick: Pick | null; onClose: () => void }) {
  return (
    <Dialog open={pick !== null} onOpenChange={(open) => !open && onClose()}>
      {/* Keyed by pick so every title starts with nothing selected. */}
      {pick && <AcceptFlow key={pick.id} pick={pick} onClose={onClose} />}
    </Dialog>
  );
}

/** Follows the live pick: choose a profile, wait for Radarr's search, and offer a switch when nothing fit. */
function AcceptFlow({ pick: initial, onClose }: { pick: Pick; onClose: () => void }) {
  const cached = usePick(initial.id, initial.kind).pick;
  const [added, setAdded] = useState<Pick | null>(null);
  // A check this session started keeps the dialog on it, also when the title is opened again.
  const [started, setStarted] = useState(() => isAwaitingRelease(initial.id));
  const [switched, setSwitched] = useState(false);
  const pick = cached ?? added ?? initial;
  const release = pick.request?.release;
  const canSwitch = release?.status === "waiting" && release.alternatives.length > 0;
  useReleaseShown(pick.id);

  useEffect(() => {
    if (!started || !release || release.status === "checking") return;
    const prefix = takeAwaitedRelease(pick.id);
    if (canSwitch) return;
    if (prefix) announceRelease(prefix, release);
    onClose();
  }, [started, release, canSwitch, pick.id, onClose]);

  if (release && canSwitch) {
    return (
      <SwitchForm
        pick={pick}
        check={release}
        switched={switched}
        onSwitched={() => {
          setStarted(true);
          setSwitched(true);
        }}
        onClose={onClose}
      />
    );
  }
  if (release && (release.status === "checking" || started)) return <Searching pick={pick} profile={release.profile} onClose={onClose} />;
  return (
    <AcceptForm
      pick={pick}
      onAdded={(p) => {
        setAdded(p);
        setStarted(true);
      }}
      onClose={onClose}
    />
  );
}

function AcceptForm({ pick, onAdded, onClose }: { pick: Pick; onAdded: (pick: Pick) => void; onClose: () => void }) {
  const app = appFor(pick.kind);
  const name = appName(app);
  const movie = app === "radarr";
  const options = useAppOptions(app, true);
  const profileOrder = useConfig().data?.radarr?.profile_order;
  const request = useRequestPick();
  const mounted = useMounted();
  const [profileId, setProfileId] = useState<string>("");
  const [ifNothingFits, setIfNothingFits] = useState<IfNothingFits>("switch");
  const [rootFolder, setRootFolder] = useState<string>("");
  const [error, setError] = useState<string | null>(null);

  const profiles = options.data?.quality_profiles ?? [];
  const targets = fallbackTargets(profileOrder, Number(profileId), profiles);
  // Without a lower-ranked profile the server always waits, so that is what the dialog shows and sends.
  const fallback: IfNothingFits = targets.available ? ifNothingFits : "wait";
  const folders = options.data?.root_folders ?? [];
  const needsFolder = folders.length > 1 && !options.data?.default_root_folder;
  const canSubmit = profileId !== "" && (!needsFolder || rootFolder !== "") && !request.isPending;

  const submit = async () => {
    if (!canSubmit) return;
    setError(null);
    const profile = profiles.find((p) => String(p.id) === profileId);
    const prefix = `Added to ${name}`;
    try {
      const updated = await request.mutateAsync({
        pick,
        qualityProfileId: Number(profileId),
        rootFolder: needsFolder ? rootFolder : undefined,
        ifNothingFits: movie ? fallback : undefined,
      });
      const release = updated.request?.release;
      if (release?.status === "checking") {
        // Reported by the dialog if it is still open, otherwise by a toast when pick.updated arrives.
        awaitRelease(pick.id, prefix);
        onAdded(updated);
        return;
      }
      if (release) announceRelease(prefix, release);
      else toast.success(`${prefix} · ${updated.request?.quality_profile ?? profile?.name ?? ""}`, { description: pick.title });
    } catch (err) {
      if (err instanceof ApiError && err.status === 409) {
        toast("Already in your library", { description: `${pick.title} is already in ${name}.` });
      } else if (mounted.current) {
        setError((err as Error).message);
        return;
      } else {
        toast.error(`Could not add ${pick.title}`, { description: (err as Error).message });
      }
    }
    if (mounted.current) onClose();
  };

  return (
    <DialogContent aria-describedby="accept-description" onOpenAutoFocus={(e) => e.preventDefault()}>
      <form
        className="flex min-h-0 flex-col"
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <TitleHeader pick={pick}>Choose the quality profile {name} uses for this title. It is asked for every title you add.</TitleHeader>

        <div className="min-h-0 overflow-y-auto px-5 py-5">
          <fieldset disabled={request.isPending}>
            <legend className="mb-3 text-sm font-semibold">Quality profile</legend>
            {options.isPending && (
              <div className="grid gap-2" aria-busy>
                {[0, 1, 2, 3].map((i) => (
                  <div key={i} className="h-[46px] animate-pulse rounded-[var(--radius-control)] bg-surface-raised" />
                ))}
              </div>
            )}
            {options.isError && (
              <div className="flex items-start gap-2 rounded-[var(--radius-control)] border border-danger/35 bg-danger/8 p-3 text-sm">
                <AlertCircle className="mt-0.5 size-4 shrink-0 text-danger" />
                <div className="min-w-0 flex-1">
                  <p className="font-medium">Could not load {name} quality profiles</p>
                  <p className="mt-0.5 break-words text-text-muted">{options.error.message}</p>
                </div>
                <Button size="sm" onClick={() => void options.refetch()}>
                  Retry
                </Button>
              </div>
            )}
            {options.data && profiles.length === 0 && (
              <p className="text-sm text-text-muted">{name} has no quality profiles. Create one in {name} first.</p>
            )}
            {profiles.length > 0 && (
              <RadioGroup value={profileId} onValueChange={setProfileId} aria-label="Quality profile" required>
                {profiles.map((p) => (
                  <RadioCard key={p.id} value={String(p.id)}>
                    {p.name}
                  </RadioCard>
                ))}
              </RadioGroup>
            )}
          </fieldset>

          {movie && profiles.length > 0 && (
            <fieldset className="mt-6" disabled={request.isPending}>
              <legend className="mb-3 text-sm font-semibold">If nothing fits this profile</legend>
              <RadioGroup
                value={fallback}
                onValueChange={(v) => setIfNothingFits(v as IfNothingFits)}
                aria-label="If nothing fits this profile"
                aria-describedby="if-nothing-fits-hint"
              >
                <RadioCard value="switch" disabled={!targets.available} className="disabled:cursor-not-allowed disabled:opacity-50">
                  Switch to the next lower-ranked profile that finds it
                </RadioCard>
                <RadioCard value="wait">Keep waiting</RadioCard>
              </RadioGroup>
              <p id="if-nothing-fits-hint" className="mt-2 text-sm text-text-muted">
                {targets.available ? (
                  `Tries ${targets.lower.map((p) => p.name).join(", then ")}.`
                ) : profileOrder && profileId === "" ? (
                  "Choose a quality profile to see what it can fall back to."
                ) : targets.reason === "last" ? (
                  "Nothing is ranked below this profile."
                ) : (
                  <>
                    Rank your quality profiles under{" "}
                    <Link to="/connections" className="text-text underline decoration-border underline-offset-4 hover:decoration-text">
                      Connections
                    </Link>{" "}
                    to fall back automatically.
                  </>
                )}
              </p>
            </fieldset>
          )}

          {needsFolder && (
            <div className="mt-6">
              <label className="mb-3 block text-sm font-semibold" id="root-folder-label">
                Root folder
              </label>
              <Select value={rootFolder} onValueChange={setRootFolder} label="Root folder" placeholder="Choose where it goes" className="w-full">
                {folders.map((f) => (
                  <SelectItem key={f.id} value={f.path}>
                    <span className="flex items-center gap-2">
                      <HardDrive className="size-4 text-text-muted" />
                      {f.path}
                      {f.free_space > 0 && <span className="text-text-muted">{gigabytes(f.free_space)}</span>}
                    </span>
                  </SelectItem>
                ))}
              </Select>
            </div>
          )}

          {error && <InlineError message={error} />}
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant={canSubmit || request.isPending ? "primary" : "secondary"} disabled={!canSubmit}>
            {request.isPending ? (
              <>
                <Loader2 className="animate-spin" /> Adding and searching…
              </>
            ) : (
              `Add to ${name}`
            )}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  );
}

/** While Proposarr follows Radarr's search. Closing is fine; the result then arrives as a toast. */
function Searching({ pick, profile, onClose }: { pick: Pick; profile: string; onClose: () => void }) {
  return (
    <DialogContent aria-describedby="accept-description" onOpenAutoFocus={(e) => e.preventDefault()}>
      <TitleHeader pick={pick}>Added to Radarr. You can close this; Proposarr tells you what Radarr finds.</TitleHeader>
      <p role="status" className="flex items-center gap-3 px-5 py-8 text-sm">
        <Loader2 className="size-5 shrink-0 animate-spin text-accent" />
        <span>
          Radarr is searching with <strong className="font-semibold">{profile}</strong>…
        </span>
      </p>
      <DialogFooter>
        <Button onClick={onClose}>Close</Button>
      </DialogFooter>
    </DialogContent>
  );
}

/** Radarr grabbed nothing, but other profiles would grab a release now. */
function SwitchForm({
  pick,
  check,
  switched,
  onSwitched,
  onClose,
}: {
  pick: Pick;
  check: ReleaseCheck;
  switched: boolean;
  onSwitched: () => void;
  onClose: () => void;
}) {
  const switchProfile = useSwitchProfile();
  const mounted = useMounted();
  const [profileId, setProfileId] = useState<string>("");
  const [error, setError] = useState<string | null>(null);

  const chosen = check.alternatives.find((a) => String(a.id) === profileId);
  const canSubmit = chosen !== undefined && !switchProfile.isPending;

  const submit = async () => {
    if (!chosen || !canSubmit) return;
    setError(null);
    const prefix = `Switched to ${chosen.name}`;
    try {
      const updated = await switchProfile.mutateAsync({ pick, qualityProfileId: chosen.id });
      const release = updated.request?.release;
      if (release?.status === "checking") {
        awaitRelease(pick.id, prefix);
        onSwitched();
        return;
      }
      if (release) announceRelease(prefix, release);
      else toast.success(prefix, { description: pick.title });
    } catch (err) {
      if (mounted.current) {
        setError((err as Error).message);
        return;
      }
      toast.error(`Could not switch ${pick.title} to ${chosen.name}`, { description: (err as Error).message });
    }
    if (mounted.current) onClose();
  };

  const strong = "font-semibold text-text";

  return (
    <DialogContent aria-describedby="accept-description" onOpenAutoFocus={(e) => e.preventDefault()}>
      <form
        className="flex min-h-0 flex-col"
        onSubmit={(e) => {
          e.preventDefault();
          void submit();
        }}
      >
        <TitleHeader pick={pick}>Radarr keeps monitoring and grabs a release once one fits. Or switch to a profile that would grab one now.</TitleHeader>

        <div className="min-h-0 overflow-y-auto px-5 py-5">
          <div role="status" className="flex items-start gap-2 rounded-[var(--radius-control)] border border-warning/35 bg-warning/8 p-3 text-sm">
            <Clock className="mt-0.5 size-4 shrink-0 text-warning" />
            <div className="min-w-0 flex-1">
              <p className="font-medium">{switched ? "Switched · still waiting for a release" : "Added · waiting for a release"}</p>
              <p className="mt-0.5 text-text-muted">
                {check.switched_from ? (
                  <>
                    Nothing fit <strong className={strong}>{check.switched_from}</strong>. Switched to <strong className={strong}>{check.profile}</strong>, but
                    Radarr still didn't grab anything.
                  </>
                ) : check.found === 0 ? (
                  "No releases on your indexers yet."
                ) : (
                  <>
                    {check.found} found, none fit <strong className={strong}>{check.profile}</strong>
                    {switched && " either"}.
                  </>
                )}
              </p>
              {check.qualities.length > 0 && (
                <p className="nums mt-1.5 text-text-muted">
                  {check.qualities.map((q, i) => (
                    <Fragment key={q.quality}>
                      {i > 0 && " · "}
                      <span className="whitespace-nowrap">
                        {q.count} × {q.quality}
                      </span>
                    </Fragment>
                  ))}
                </p>
              )}
            </div>
          </div>

          {/* min-w-0: a fieldset is min-content wide by default, which stops long release titles truncating. */}
          <fieldset className="mt-6 min-w-0" disabled={switchProfile.isPending}>
            <legend className="mb-3 text-sm font-semibold">Available now with another profile</legend>
            <RadioGroup value={profileId} onValueChange={setProfileId} aria-label="Quality profile" required>
              {check.alternatives.map((a) => (
                <RadioCard key={a.id} value={String(a.id)} className="min-w-0">
                  <span className="flex items-baseline justify-between gap-3">
                    <span className="font-medium">{a.name}</span>
                    <span className="nums shrink-0 text-xs text-text-muted">{a.count === 1 ? "1 release" : `${a.count} releases`}</span>
                  </span>
                  <span className="mt-1 flex gap-1 text-xs text-text-muted" title={a.best.title}>
                    <span className="nums shrink-0">{[a.best.quality, fileSize(a.best.size)].filter(Boolean).join(" · ")} ·</span>
                    <span className="truncate">{a.best.title}</span>
                  </span>
                </RadioCard>
              ))}
            </RadioGroup>
          </fieldset>

          {error && <InlineError message={error} />}
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Keep waiting
          </Button>
          <Button type="submit" variant={canSubmit || switchProfile.isPending ? "primary" : "secondary"} disabled={!canSubmit}>
            {switchProfile.isPending ? (
              <>
                <Loader2 className="animate-spin" /> Switching…
              </>
            ) : (
              "Switch and search"
            )}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  );
}

function TitleHeader({ pick, children }: { pick: Pick; children: ReactNode }) {
  return (
    <div className="flex gap-4 border-b border-border px-5 pt-5 pb-4 pr-14">
      <Poster src={pick.poster_url} title={pick.title} className="w-[72px] shrink-0 rounded-md" />
      <div className="flex min-w-0 flex-col justify-end">
        <DialogTitle className="line-clamp-2">
          {pick.title}
          {pick.year && <span className="nums ml-2 text-text-muted">{pick.year}</span>}
        </DialogTitle>
        <TitleLinks pick={pick} tone="surface" className="mt-2.5" />
        <DialogDescription id="accept-description" className="mt-2.5">
          {children}
        </DialogDescription>
      </div>
    </div>
  );
}

function InlineError({ message }: { message: string }) {
  return (
    <p role="alert" className="mt-5 flex items-start gap-2 rounded-[var(--radius-control)] border border-danger/35 bg-danger/8 p-3 text-sm">
      <AlertCircle className="mt-0.5 size-4 shrink-0 text-danger" />
      <span className="break-words">{message}</span>
    </p>
  );
}

/** False once the dialog has closed or moved to another title. */
function useMounted() {
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  return mounted;
}
