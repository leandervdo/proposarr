import { useCallback, useMemo, useState } from "react";
import { ApiError } from "@/api/client";
import { useSaveSettings, useTestService } from "@/api/queries";
import type { Service, SettingField, SettingValues, Settings, TestResult } from "@/api/types";

export interface ServiceForm {
  settings: Settings;
  field: (key: string) => SettingField | undefined;
  /** The value shown in an input: the draft when edited, else the saved value. */
  value: (key: string) => string;
  /** undefined: unchanged; null: remove on save; string: new value. */
  draftOf: (key: string) => string | number | null | undefined;
  set: (key: string, value: string | number | null) => void;
  revert: (key: string) => void;
  error: (key: string) => string | undefined;
  draft: SettingValues;
  dirty: boolean;
  reset: () => void;
  save: () => Promise<Settings | null>;
  saving: boolean;
  saveError: string | null;
  test: (service: Service) => Promise<TestResult | null>;
  testing: boolean;
  testResult: TestResult | null;
}

function saved(f: SettingField | undefined): string {
  return f?.value === undefined || f.value === null ? "" : String(f.value);
}

/** Draft state for a group of settings keys, saved and tested together. */
export function useServiceForm(settings: Settings, keys: readonly string[]): ServiceForm {
  const [draft, setDraft] = useState<SettingValues>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [saveError, setSaveError] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<TestResult | null>(null);
  const saveMutation = useSaveSettings();
  const testMutation = useTestService();

  const field = useCallback((key: string) => settings.fields[key], [settings]);

  const set = useCallback(
    (key: string, value: string | number | null) => {
      const f = settings.fields[key];
      setDraft((d) => {
        const next = { ...d };
        // Typing back the saved value is not a change; secrets have no visible saved value.
        if (f && !f.secret && value !== null && String(value) === saved(f)) delete next[key];
        else if (f?.secret && value === "") delete next[key];
        else next[key] = value;
        return next;
      });
      setErrors((e) => {
        if (!(key in e)) return e;
        const next = { ...e };
        delete next[key];
        return next;
      });
      setTestResult(null);
    },
    [settings],
  );

  const revert = useCallback((key: string) => {
    setDraft((d) => {
      const next = { ...d };
      delete next[key];
      return next;
    });
  }, []);

  const reset = useCallback(() => {
    setDraft({});
    setErrors({});
    setSaveError(null);
  }, []);

  const save = useCallback(async () => {
    setSaveError(null);
    const values = Object.fromEntries(Object.entries(draft).filter(([k]) => keys.includes(k)));
    try {
      const next = await saveMutation.mutateAsync(values);
      setDraft({});
      setErrors({});
      return next;
    } catch (err) {
      if (err instanceof ApiError && Object.keys(err.fieldErrors).length > 0) {
        setErrors(err.fieldErrors);
        const outside = Object.keys(err.fieldErrors).filter((k) => !keys.includes(k));
        setSaveError(outside.length ? err.message : null);
      } else {
        setSaveError(err instanceof Error ? err.message : String(err));
      }
      return null;
    }
  }, [draft, keys, saveMutation]);

  const test = useCallback(
    async (service: Service) => {
      setTestResult(null);
      try {
        const result = await testMutation.mutateAsync({ service, values: draft });
        setTestResult(result);
        return result;
      } catch (err) {
        const result: TestResult = { status: "fail", detail: err instanceof Error ? err.message : String(err) };
        setTestResult(result);
        return result;
      }
    },
    [draft, testMutation],
  );

  return useMemo(
    () => ({
      settings,
      field,
      value: (key: string) => (key in draft ? (draft[key] === null ? "" : String(draft[key])) : saved(settings.fields[key])),
      draftOf: (key: string) => draft[key],
      set,
      revert,
      error: (key: string) => errors[key],
      draft,
      dirty: Object.keys(draft).length > 0,
      reset,
      save,
      saving: saveMutation.isPending,
      saveError,
      test,
      testing: testMutation.isPending,
      testResult,
    }),
    [settings, field, draft, set, revert, errors, reset, save, saveMutation.isPending, saveError, test, testMutation.isPending, testResult],
  );
}
