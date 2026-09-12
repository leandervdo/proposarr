import { toast } from "sonner";
import { useSetVerdict } from "@/api/queries";
import type { Pick } from "@/api/types";
import { mapLimit } from "@/lib/concurrency";
import { plural } from "@/lib/format";

const CONCURRENCY = 4;

/** Later / Ignore for several picks at once: one toast, whose Undo gives every pick its previous verdict back. */
export function useBulkVerdict() {
  const setVerdict = useSetVerdict();

  return async (picks: Pick[], verdict: "later" | "ignored", note?: string) => {
    if (picks.length === 0) return;
    const results = await mapLimit(picks, CONCURRENCY, (pick) => setVerdict.mutateAsync({ pick, verdict }));
    const done = picks.filter((_, i) => results[i]!.status === "fulfilled");
    const failed = failures(picks, results);

    if (done.length === 0) {
      toast.error(`Could not ${verdict === "later" ? "save" : "ignore"} ${plural(picks.length, "title")}`, { description: failed });
      return;
    }

    const undo = async () => {
      // `done` holds the picks as they were, so each one's verdict is what it goes back to.
      const back = await mapLimit(done, CONCURRENCY, (pick) =>
        setVerdict.mutateAsync({ pick: { ...pick, verdict }, verdict: pick.verdict ?? "" }),
      );
      const undoFailed = back.filter((r) => r.status === "rejected").length;
      if (undoFailed > 0) toast.error(`Could not undo ${plural(undoFailed, "title")}`, { description: failures(done, back) });
    };

    const title = verdict === "later" ? `Saved ${done.length} for later` : `Ignored ${done.length}`;
    const description = [failed ? `Could not update ${failed}` : titleList(done), note].filter(Boolean).join(". ");
    const options = { description, action: { label: "Undo", onClick: () => void undo() } };
    if (failed) toast.warning(title, options);
    else toast(title, options);
  };
}

/** "Her, Moon and 3 more" */
function titleList(picks: Pick[]): string {
  const names = picks.slice(0, 3).map((p) => p.title);
  const more = picks.length - names.length;
  if (more > 0) return `${names.join(", ")} and ${more} more`;
  if (names.length <= 1) return names.join("");
  return `${names.slice(0, -1).join(", ")} and ${names.at(-1)}`;
}

/** "Her, Moon: <first error>", or "" when everything went through. */
function failures(picks: Pick[], results: PromiseSettledResult<unknown>[]): string {
  const failed = picks.filter((_, i) => results[i]!.status === "rejected");
  if (failed.length === 0) return "";
  const first = results.find((r): r is PromiseRejectedResult => r.status === "rejected");
  const message = first?.reason instanceof Error ? first.reason.message : "";
  return `${titleList(failed)}${message ? `: ${message}` : ""}`;
}
