import { AlertCircle, HardDrive, Loader2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { ApiError } from "@/api/client";
import { useAppOptions, useRequestPick } from "@/api/queries";
import type { Pick } from "@/api/types";
import { appFor, appName, gigabytes } from "@/lib/format";
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
      {pick && <AcceptForm key={pick.id} pick={pick} onClose={onClose} />}
    </Dialog>
  );
}

function AcceptForm({ pick, onClose }: { pick: Pick; onClose: () => void }) {
  const app = appFor(pick.kind);
  const name = appName(app);
  const options = useAppOptions(app, true);
  const request = useRequestPick();
  const [profileId, setProfileId] = useState<string>("");
  const [rootFolder, setRootFolder] = useState<string>("");
  const [error, setError] = useState<string | null>(null);

  const folders = options.data?.root_folders ?? [];
  const needsFolder = folders.length > 1 && !options.data?.default_root_folder;
  const canSubmit = profileId !== "" && (!needsFolder || rootFolder !== "") && !request.isPending;

  const submit = () => {
    if (!canSubmit) return;
    setError(null);
    const profile = options.data?.quality_profiles.find((p) => String(p.id) === profileId);
    request.mutate(
      { pick, qualityProfileId: Number(profileId), rootFolder: needsFolder ? rootFolder : undefined },
      {
        onSuccess: (updated) => {
          toast.success(`Added to ${name} · ${updated.request?.quality_profile ?? profile?.name ?? ""}`, { description: pick.title });
          onClose();
        },
        onError: (err) => {
          if (err instanceof ApiError && err.status === 409) {
            toast("Already in your library", { description: `${pick.title} is already in ${name}.` });
            onClose();
            return;
          }
          setError(err.message);
        },
      },
    );
  };

  return (
    <DialogContent aria-describedby="accept-description" onOpenAutoFocus={(e) => e.preventDefault()}>
      <form
        className="flex min-h-0 flex-col"
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
      >
        <div className="flex gap-4 border-b border-border px-5 pt-5 pb-4 pr-14">
          <Poster src={pick.poster_url} title={pick.title} className="w-[72px] shrink-0 rounded-md" />
          <div className="flex min-w-0 flex-col justify-end">
            <DialogTitle className="line-clamp-2">
              {pick.title}
              {pick.year && <span className="nums ml-2 text-text-muted">{pick.year}</span>}
            </DialogTitle>
            <TitleLinks pick={pick} tone="surface" className="mt-2.5" />
            <DialogDescription id="accept-description" className="mt-2.5">
              Choose the quality profile {name} uses for this title. It is asked for every title you add.
            </DialogDescription>
          </div>
        </div>

        <div className="min-h-0 overflow-y-auto px-5 py-5">
          <fieldset>
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
            {options.data && options.data.quality_profiles.length === 0 && (
              <p className="text-sm text-text-muted">{name} has no quality profiles. Create one in {name} first.</p>
            )}
            {options.data && options.data.quality_profiles.length > 0 && (
              <RadioGroup value={profileId} onValueChange={setProfileId} aria-label="Quality profile" required>
                {options.data.quality_profiles.map((p) => (
                  <RadioCard key={p.id} value={String(p.id)}>
                    {p.name}
                  </RadioCard>
                ))}
              </RadioGroup>
            )}
          </fieldset>

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

          {error && (
            <p role="alert" className="mt-5 flex items-start gap-2 rounded-[var(--radius-control)] border border-danger/35 bg-danger/8 p-3 text-sm">
              <AlertCircle className="mt-0.5 size-4 shrink-0 text-danger" />
              <span className="break-words">{error}</span>
            </p>
          )}
        </div>

        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" variant={canSubmit || request.isPending ? "primary" : "secondary"} disabled={!canSubmit}>
            {request.isPending ? (
              <>
                <Loader2 className="animate-spin" /> Adding…
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
