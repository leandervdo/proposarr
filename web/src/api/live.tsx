import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { settleRelease } from "@/lib/releases";
import { subscribe } from "./events";
import { applyPick, keys } from "./queries";
import type { Kind, Pick, Run } from "./types";

interface LiveRun {
  runId: number;
  kind: Kind;
  vibe?: string;
  /** Unknown when only progress events were seen. */
  useTaste?: boolean;
  startedAt?: string;
  message: string;
}

interface LiveState {
  connected: boolean;
  running: Record<number, LiveRun>;
  /** Most recent finished run from this session, for toasts. */
  lastFinished: Run | null;
}

const LiveContext = createContext<LiveState>({ connected: false, running: {}, lastFinished: null });

export function LiveProvider({ children }: { children: ReactNode }) {
  const qc = useQueryClient();
  const [connected, setConnected] = useState(false);
  const [running, setRunning] = useState<Record<number, LiveRun>>({});
  const [lastFinished, setLastFinished] = useState<Run | null>(null);

  useEffect(
    () =>
      subscribe((e) => {
        switch (e.type) {
          case "run.started":
            setRunning((r) => ({
              ...r,
              [e.data.id]: { runId: e.data.id, kind: e.data.kind, vibe: e.data.vibe, useTaste: e.data.use_taste, startedAt: e.data.started_at, message: "Starting…" },
            }));
            void qc.invalidateQueries({ queryKey: keys.runs });
            break;
          case "run.progress":
            setRunning((r) => ({
              ...r,
              [e.data.run_id]: { ...r[e.data.run_id], runId: e.data.run_id, kind: e.data.kind, message: e.data.message },
            }));
            break;
          case "run.finished":
            setRunning((r) => {
              const next = { ...r };
              delete next[e.data.id];
              return next;
            });
            setLastFinished(e.data);
            void qc.invalidateQueries({ queryKey: keys.runs });
            void qc.invalidateQueries({ queryKey: keys.status });
            void qc.invalidateQueries({ queryKey: ["picks"] });
            void qc.invalidateQueries({ queryKey: ["library"] });
            break;
          case "pick.updated":
            queuePickEvent(qc, e.data);
            break;
        }
      }, setConnected),
    [qc],
  );

  const value = useMemo(() => ({ connected, running, lastFinished }), [connected, running, lastFinished]);
  return <LiveContext.Provider value={value}>{children}</LiveContext.Provider>;
}

export const useLive = () => useContext(LiveContext);

/** pick.updated events are applied one after another, so a later event never lands before an earlier one. */
let pickEvents = Promise.resolve();

function queuePickEvent(qc: QueryClient, pick: Pick) {
  pickEvents = pickEvents.then(() => applyPickEvent(qc, pick)).catch(() => {});
}

/**
 * Writes a pick.updated event into every cached list. A picks refetch that started before the event can
 * resolve after it and put the old state back (a release check back on "checking"), so in-flight picks
 * queries are cancelled first (reverting to their data from before the fetch, manual writes included), then
 * the event's pick is written, and the cancelled lists are refetched: the server already has the new state.
 */
async function applyPickEvent(qc: QueryClient, pick: Pick) {
  const inFlight = qc.isFetching({ queryKey: ["picks"] }) > 0;
  await qc.cancelQueries({ queryKey: ["picks"] });
  applyPick(qc, pick);
  settleRelease(pick);
  if (inFlight) void qc.invalidateQueries({ queryKey: ["picks"] });
}
