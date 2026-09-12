import { toast } from "sonner";
import { useSetVerdict } from "@/api/queries";
import type { Pick, Verdict } from "@/api/types";

/** Later / Ignore / Move back with an undo toast, shared by the pick cards and the title modal. */
export function useVerdictActions() {
  const setVerdict = useSetVerdict();

  const decide = (pick: Pick, verdict: Verdict | "") => {
    const previous = pick.verdict ?? "";
    setVerdict.mutate(
      { pick, verdict },
      {
        onSuccess: () => {
          const label = verdict === "later" ? "Saved for later" : verdict === "ignored" ? "Ignored" : "Moved back to undecided";
          toast(label, {
            description: pick.title,
            action: { label: "Undo", onClick: () => setVerdict.mutate({ pick: { ...pick, verdict: verdict || undefined }, verdict: previous }) },
          });
        },
        onError: (err) => toast.error(`Could not update ${pick.title}`, { description: err.message }),
      },
    );
  };

  return { decide, isPending: setVerdict.isPending };
}
