import type { Pick, ProgressEvent, Run } from "./types";

export type LiveEvent =
  | { type: "run.started"; data: Run }
  | { type: "run.progress"; data: ProgressEvent }
  | { type: "run.finished"; data: Run }
  | { type: "pick.updated"; data: Pick };

const EVENT_TYPES = ["run.started", "run.progress", "run.finished", "pick.updated"] as const;

/** Subscribes to /api/events. Returns an unsubscribe function. */
export function subscribe(onEvent: (e: LiveEvent) => void, onConnection: (connected: boolean) => void): () => void {
  if (import.meta.env.VITE_MOCK === "1") {
    let unsubscribe = () => {};
    let cancelled = false;
    void import("./mock").then((m) => {
      if (cancelled) return;
      onConnection(m.mockConnected());
      unsubscribe = m.mockSubscribe(onEvent);
    });
    return () => {
      cancelled = true;
      unsubscribe();
    };
  }

  const source = new EventSource("/api/events");
  for (const type of EVENT_TYPES) {
    source.addEventListener(type, (ev) => {
      try {
        onEvent({ type, data: JSON.parse((ev as MessageEvent<string>).data) } as LiveEvent);
      } catch {
        // Ignore malformed frames.
      }
    });
  }
  source.onopen = () => onConnection(true);
  // EventSource reconnects on its own; report the gap meanwhile.
  source.onerror = () => onConnection(false);
  return () => source.close();
}
