import { useCallback, useEffect, useMemo, useState } from "react";

export interface Selection<T extends { id: number }> {
  /** Selected items that are still visible, in visible order. */
  selected: T[];
  count: number;
  /** Number of visible items. */
  total: number;
  /** Something is selected: a click on a card toggles it instead of opening the details. */
  active: boolean;
  allSelected: boolean;
  isSelected: (id: number) => boolean;
  /** Toggles one item. With `range`, selects every visible item between the last clicked one and this one. */
  toggle: (id: number, range?: boolean) => void;
  selectAll: () => void;
  clear: () => void;
}

interface State {
  key: string;
  ids: ReadonlySet<number>;
  /** The last clicked item, where a shift+click range starts. */
  anchor: number | null;
}

const NONE: ReadonlySet<number> = new Set();

/**
 * Multi-select over a list of visible items (Picks grid, Collection searches). Starts empty again whenever
 * `resetKey` changes (kind, run, filters, tab). Esc clears it unless a dialog is open.
 */
export function useSelection<T extends { id: number }>(items: readonly T[], resetKey: string): Selection<T> {
  const [state, setState] = useState<State>({ key: resetKey, ids: NONE, anchor: null });
  // Kept under the key it was made for, so a new key reads as empty without an effect.
  const ids = state.key === resetKey ? state.ids : NONE;

  const selected = useMemo(() => items.filter((item) => ids.has(item.id)), [items, ids]);
  const count = selected.length;

  const toggle = useCallback(
    (id: number, range = false) =>
      setState((prev) => {
        const base = prev.key === resetKey ? prev : { key: resetKey, ids: NONE, anchor: null };
        const next = new Set(base.ids);
        if (range && base.anchor !== null) {
          const from = items.findIndex((item) => item.id === base.anchor);
          const to = items.findIndex((item) => item.id === id);
          if (from !== -1 && to !== -1) {
            for (let i = Math.min(from, to); i <= Math.max(from, to); i++) next.add(items[i]!.id);
            return { key: resetKey, ids: next, anchor: id };
          }
        }
        if (next.has(id)) next.delete(id);
        else next.add(id);
        return { key: resetKey, ids: next, anchor: id };
      }),
    [items, resetKey],
  );

  const selectAll = useCallback(
    () => setState((prev) => ({ key: resetKey, ids: new Set(items.map((item) => item.id)), anchor: prev.key === resetKey ? prev.anchor : null })),
    [items, resetKey],
  );

  const clear = useCallback(() => setState({ key: resetKey, ids: NONE, anchor: null }), [resetKey]);

  useEffect(() => {
    if (count === 0) return;
    const onKeyDown = (e: KeyboardEvent) => {
      // An open dialog, select or menu takes Esc for itself.
      if (e.key !== "Escape" || e.defaultPrevented) return;
      if (document.querySelector('[role="dialog"], [role="alertdialog"]')) return;
      clear();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [count, clear]);

  const isSelected = useCallback((id: number) => ids.has(id), [ids]);

  return {
    selected,
    count,
    total: items.length,
    active: count > 0,
    allSelected: items.length > 0 && count === items.length,
    isSelected,
    toggle,
    selectAll,
    clear,
  };
}
