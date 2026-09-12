import { useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Check, Loader2, Moon, Sun } from "lucide-react";
import { AnimatePresence, motion } from "motion/react";
import { createContext, useContext, useState, type ReactNode } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { toast, Toaster } from "sonner";
import { keys, useSettings, useStartRun, useStatus } from "@/api/queries";
import type { Service, Settings, TestResult } from "@/api/types";
import { ErrorNote } from "@/components/EmptyState";
import { ProfileRankingField } from "@/components/settings/ProfileRankingField";
import { SERVICE_KEYS, SERVICE_META, ServiceFields, TestNote } from "@/components/settings/services";
import { useServiceForm, type ServiceForm } from "@/components/settings/useServiceForm";
import { Button } from "@/components/ui/button";
import { Segmented } from "@/components/ui/segmented";
import { useTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";

const STEPS = [
  { id: "welcome", label: "Welcome" },
  { id: "library", label: "Library" },
  { id: "profiles", label: "Quality ranking" },
  { id: "tmdb", label: "TMDB" },
  { id: "history", label: "Watch history" },
  { id: "claude", label: "Claude" },
  { id: "done", label: "Done" },
] as const;
type StepId = (typeof STEPS)[number]["id"];

const RANKING_KEYS = ["radarr.profile_order"] as const;

/** The ranking step only exists once Radarr is configured. */
function stepsFor(settings: Settings | undefined) {
  return STEPS.filter((s) => s.id !== "profiles" || (settings !== undefined && isSet(settings, "radarr.url")));
}

/** "Step 3 of 7", for the step headings. */
const StepCount = createContext("");

const MISSING_LABEL: Record<string, string> = {
  "radarr.url or sonarr.url": "Radarr or Sonarr",
  "tmdb.api_key": "TMDB API key",
  "claude.oauth_token or claude.api_key": "Claude credential",
};

function isSet(settings: Settings, key: string) {
  const f = settings.fields[key];
  return !!f?.set;
}

export function SetupPage() {
  const [params, setParams] = useSearchParams();
  const qc = useQueryClient();
  const settings = useSettings();
  const status = useStatus();
  const steps = stepsFor(settings.data);
  const stepId = (steps.find((s) => s.id === params.get("step"))?.id ?? "welcome") as StepId;
  const index = steps.findIndex((s) => s.id === stepId);
  const [direction, setDirection] = useState(1);
  const [theme, toggleTheme] = useTheme();

  // Read the latest settings: saving Radarr adds the ranking step before this component re-renders.
  const latestSteps = () => stepsFor(qc.getQueryData<Settings>(keys.settings));
  const go = (id: StepId) => {
    const list = latestSteps();
    setDirection(list.findIndex((s) => s.id === id) >= list.findIndex((s) => s.id === stepId) ? 1 : -1);
    setParams(id === "welcome" ? {} : { step: id });
    window.scrollTo({ top: 0 });
  };
  const next = () => {
    const list = latestSteps();
    go(list[Math.min(list.findIndex((s) => s.id === stepId) + 1, list.length - 1)]!.id);
  };
  const back = () => {
    const list = latestSteps();
    go(list[Math.max(list.findIndex((s) => s.id === stepId) - 1, 0)]!.id);
  };

  const missing = (status.data?.missing ?? []).map((m) => MISSING_LABEL[m] ?? m);

  return (
    <div className="min-h-dvh bg-background text-text lg:grid lg:grid-cols-[21rem_1fr]">
      {/* Steps rail */}
      <aside className="sticky top-0 hidden h-dvh flex-col border-r border-border px-8 pt-9 pb-8 lg:flex">
        <p className="font-display text-[34px] leading-none font-bold tracking-tight">
          Propos<span className="text-accent">arr</span>
        </p>
        <p className="mt-2 text-sm text-text-muted">First-time setup</p>
        <ol className="mt-10 flex flex-col gap-1">
          {steps.map((s, i) => {
            const done = i < index;
            const current = i === index;
            return (
              <li key={s.id}>
                <button
                  type="button"
                  onClick={() => i <= index && go(s.id)}
                  disabled={i > index}
                  aria-current={current ? "step" : undefined}
                  className={cn(
                    "flex h-11 w-full items-center gap-3 rounded-[var(--radius-control)] px-2 text-left text-[15px] transition-colors disabled:cursor-default",
                    current ? "bg-surface-raised font-semibold" : done ? "text-text hover:bg-surface" : "text-text-muted",
                  )}
                >
                  <span
                    className={cn(
                      "nums grid size-7 shrink-0 place-items-center rounded-full border text-[13px]",
                      done && "border-success/60 bg-success/10 text-success",
                      current && "border-accent bg-accent text-accent-contrast",
                      !done && !current && "border-border",
                    )}
                  >
                    {done ? <Check className="size-3.5" strokeWidth={3} /> : i + 1}
                  </span>
                  {s.label}
                </button>
              </li>
            );
          })}
        </ol>
        <div className="mt-auto">
          {missing.length > 0 ? (
            <div className="rounded-[var(--radius-control)] border border-border p-4 text-sm">
              <p className="font-semibold">Still needed</p>
              <ul className="mt-2 flex flex-col gap-1 text-text-muted">
                {missing.map((m) => (
                  <li key={m} className="flex items-center gap-2">
                    <span className="size-1.5 rounded-full bg-warning" /> {m}
                  </li>
                ))}
              </ul>
            </div>
          ) : status.data ? (
            <p className="flex items-center gap-2 text-sm text-success">
              <Check className="size-4" /> Everything required is set
            </p>
          ) : null}
          <div className="mt-4 flex items-center justify-between">
            <Link to="/connections" className="text-sm text-text-muted underline decoration-border underline-offset-4 hover:text-text">
              Skip to connections
            </Link>
            <ThemeToggle theme={theme} onToggle={toggleTheme} />
          </div>
        </div>
      </aside>

      {/* Phone header */}
      <header className="sticky top-0 z-20 border-b border-border bg-background/90 backdrop-blur-lg lg:hidden">
        <div className="flex h-14 items-center justify-between px-4">
          <p className="font-display text-[26px] leading-none font-bold tracking-tight">
            Propos<span className="text-accent">arr</span>
          </p>
          <div className="flex items-center gap-2">
            <span className="nums text-sm text-text-muted">
              Step {index + 1} of {steps.length}
            </span>
            <ThemeToggle theme={theme} onToggle={toggleTheme} />
          </div>
        </div>
        <div className="h-0.5 bg-border">
          <div className="h-full bg-accent transition-[width] duration-300" style={{ width: `${((index + 1) / steps.length) * 100}%` }} />
        </div>
      </header>

      <main className="flex min-w-0 justify-center px-4 pt-8 pb-16 sm:px-8 lg:items-center lg:py-14">
        <div className="w-full max-w-[40rem]">
          {settings.isError ? (
            <ErrorNote title="Could not load settings" message={settings.error.message} onRetry={() => void settings.refetch()} />
          ) : !settings.data ? (
            <div className="h-96 animate-pulse rounded-[var(--radius-panel)] bg-surface" aria-busy />
          ) : (
            <StepCount.Provider value={`Step ${index + 1} of ${steps.length}`}>
              <AnimatePresence mode="wait" initial={false} custom={direction}>
                <motion.div
                  key={stepId}
                  custom={direction}
                  initial={{ opacity: 0, x: direction * 24 }}
                  animate={{ opacity: 1, x: 0 }}
                  exit={{ opacity: 0, x: direction * -24 }}
                  transition={{ duration: 0.22, ease: [0.2, 0.8, 0.2, 1] }}
                >
                  {stepId === "welcome" && <Welcome onStart={next} />}
                  {stepId === "library" && <LibraryStep settings={settings.data} onBack={back} onNext={next} />}
                  {stepId === "profiles" && <RankingStep settings={settings.data} onBack={back} onNext={next} />}
                  {stepId === "tmdb" && <SingleStep service="tmdb" settings={settings.data} onBack={back} onNext={next} required="tmdb.api_key" />}
                  {stepId === "history" && <HistoryStep settings={settings.data} onBack={back} onNext={next} />}
                  {stepId === "claude" && <ClaudeStep settings={settings.data} onBack={back} onNext={next} localLogin={status.data?.claude_auth === "local" && !(status.data?.missing ?? []).some((m) => m.startsWith("claude"))} />}
                  {stepId === "done" && <Done settings={settings.data} missing={missing} onBack={back} />}
                </motion.div>
              </AnimatePresence>
            </StepCount.Provider>
          )}
        </div>
      </main>
      <Toaster theme={theme} position="bottom-center" />
    </div>
  );
}

function ThemeToggle({ theme, onToggle }: { theme: "dark" | "light"; onToggle: () => void }) {
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-label={`Switch to ${theme === "dark" ? "light" : "dark"} theme`}
      className="grid size-9 place-items-center rounded-full text-text-muted hover:bg-surface-raised hover:text-text"
    >
      {theme === "dark" ? <Sun className="size-[18px]" /> : <Moon className="size-[18px]" />}
    </button>
  );
}

function StepHeading({ step, title, children }: { step?: boolean; title: string; children?: ReactNode }) {
  const count = useContext(StepCount);
  return (
    <div className="mb-8">
      {/* Phones show the step count in the sticky header instead. */}
      {step && <p className="mb-3 hidden text-sm text-text-muted lg:block">{count}</p>}
      <h1 className="font-display text-[44px] leading-[0.92] font-bold tracking-tight sm:text-[60px]">{title}</h1>
      {children && <div className="mt-4 max-w-[56ch] text-[16px] leading-relaxed text-text-muted">{children}</div>}
    </div>
  );
}

function Actions({ onBack, children, note }: { onBack?: () => void; children: ReactNode; note?: ReactNode }) {
  return (
    <div className="mt-8 flex flex-col-reverse gap-3 border-t border-border pt-6 sm:flex-row sm:items-center">
      {onBack && (
        <Button variant="ghost" onClick={onBack} className="self-start">
          <ArrowLeft /> Back
        </Button>
      )}
      {note && <div className="text-sm text-danger sm:mr-auto sm:ml-2">{note}</div>}
      <div className="flex flex-col gap-2 sm:ml-auto sm:flex-row">{children}</div>
    </div>
  );
}

function Welcome({ onStart }: { onStart: () => void }) {
  const items = [
    ["Your library", "Radarr, Sonarr or both, so picks skip what you own and land in the right place."],
    ["A TMDB key", "Free. Used for candidate titles, posters and where things stream."],
    ["Your watch history", "Plex or Jellyfin. Optional, but it's what makes picks personal."],
    ["Claude", "A Claude subscription token or an Anthropic API key."],
  ];
  return (
    <>
      <StepHeading title="Picks for your library, from what you actually watch">
        Proposarr reads your library and watch history and asks Claude for titles you don't have yet. You add the ones you like to Radarr or
        Sonarr, choosing the quality each time. Setup takes about five minutes, and everything is saved on this server.
      </StepHeading>
      <ul className="grid gap-3 sm:grid-cols-2">
        {items.map(([title, body]) => (
          <li key={title} className="rounded-[var(--radius-control)] border border-border bg-surface p-4">
            <p className="font-semibold">{title}</p>
            <p className="mt-1 text-sm text-text-muted">{body}</p>
          </li>
        ))}
      </ul>
      <div className="mt-8">
        <Button variant="primary" size="lg" onClick={onStart} className="w-full sm:w-auto">
          Start setup
        </Button>
      </div>
    </>
  );
}

function useTester(form: ServiceForm) {
  const [results, setResults] = useState<Partial<Record<Service, TestResult>>>({});
  const [testing, setTesting] = useState<Service | null>(null);
  const run = async (service: Service) => {
    setTesting(service);
    const r = await form.test(service);
    setTesting(null);
    if (r) setResults((prev) => ({ ...prev, [service]: r }));
  };
  return { results, testing, run };
}

function ServiceBlock({ service, form, tester, children }: { service: Service; form: ServiceForm; tester: ReturnType<typeof useTester>; children?: ReactNode }) {
  const meta = SERVICE_META[service];
  const Icon = meta.icon;
  return (
    <section className="rounded-[var(--radius-panel)] border border-border bg-surface p-5">
      <header className="mb-5 flex items-start gap-3">
        <span className="grid size-10 shrink-0 place-items-center rounded-[10px] border border-border bg-surface-raised text-text-muted">
          <Icon className="size-5" strokeWidth={1.75} />
        </span>
        <div className="min-w-0 flex-1">
          <h2 className="text-[17px] font-semibold">{meta.name}</h2>
          <p className="text-sm text-text-muted">{meta.description}</p>
        </div>
      </header>
      {children}
      {/* The ranking needs Radarr saved first, so setup asks for it in the next step. */}
      <ServiceFields service={service} form={form} ranking={false} />
      <div className="mt-4 flex flex-col gap-3">
        <TestNote result={tester.testing === service ? null : (tester.results[service] ?? null)} testing={tester.testing === service} service={service} />
        <Button size="sm" onClick={() => void tester.run(service)} disabled={tester.testing !== null} className="self-start">
          {tester.testing === service && <Loader2 className="animate-spin" />} Test {meta.name}
        </Button>
      </div>
    </section>
  );
}

function useStepSave(form: ServiceForm, onNext: () => void) {
  const [note, setNote] = useState<string | null>(null);
  const saveAndContinue = async (check?: () => string | null) => {
    setNote(null);
    const problem = check?.();
    if (problem) {
      setNote(problem);
      return;
    }
    if (form.dirty) {
      const saved = await form.save();
      if (!saved) {
        setNote("Fix the highlighted fields to continue.");
        return;
      }
    }
    onNext();
  };
  return { note, saveAndContinue };
}

function hasValue(form: ServiceForm, key: string) {
  const d = form.draftOf(key);
  if (d === null) return false;
  if (d !== undefined) return String(d).trim() !== "";
  return isSet(form.settings, key);
}

function LibraryStep({ settings, onBack, onNext }: { settings: Settings; onBack: () => void; onNext: () => void }) {
  const form = useServiceForm(settings, [...SERVICE_KEYS.radarr, ...SERVICE_KEYS.sonarr]);
  const tester = useTester(form);
  const { note, saveAndContinue } = useStepSave(form, onNext);
  const [show, setShow] = useState<"radarr" | "sonarr" | "both">(isSet(settings, "sonarr.url") && !isSet(settings, "radarr.url") ? "sonarr" : "radarr");

  return (
    <>
      <StepHeading step title="Connect your library">
        Add Radarr for movies, Sonarr for series, or both. Proposarr skips what you already own and adds accepted picks there.
      </StepHeading>
      <Segmented
        label="Which apps"
        value={show}
        onChange={setShow}
        className="mb-5"
        options={[
          { value: "radarr", label: "Radarr" },
          { value: "sonarr", label: "Sonarr" },
          { value: "both", label: "Both" },
        ]}
      />
      <div className="flex flex-col gap-5">
        {show !== "sonarr" && <ServiceBlock service="radarr" form={form} tester={tester} />}
        {show !== "radarr" && <ServiceBlock service="sonarr" form={form} tester={tester} />}
      </div>
      <Actions onBack={onBack} note={note}>
        <Button
          variant="primary"
          size="lg"
          disabled={form.saving}
          onClick={() => void saveAndContinue(() => (hasValue(form, "radarr.url") || hasValue(form, "sonarr.url") ? null : "Add the address of Radarr or Sonarr to continue."))}
        >
          {form.saving && <Loader2 className="animate-spin" />} Save and continue
        </Button>
      </Actions>
    </>
  );
}

function RankingStep({ settings, onBack, onNext }: { settings: Settings; onBack: () => void; onNext: () => void }) {
  const form = useServiceForm(settings, RANKING_KEYS);
  const { note, saveAndContinue } = useStepSave(form, onNext);
  return (
    <>
      <StepHeading step title="Rank your quality profiles">
        Put the profile you want most on top. When nothing fits the profile you chose for a movie, Proposarr can fall back to the ones ranked below it.
        You can change this later under Connections.
      </StepHeading>
      <section className="rounded-[var(--radius-panel)] border border-border bg-surface p-5">
        <ProfileRankingField form={form} />
      </section>
      <Actions onBack={onBack} note={note}>
        <Button
          variant="ghost"
          size="lg"
          onClick={() => {
            form.reset();
            onNext();
          }}
        >
          Later
        </Button>
        <Button variant="primary" size="lg" disabled={form.saving || (!form.dirty && !isSet(settings, "radarr.profile_order"))} onClick={() => void saveAndContinue()}>
          {form.saving && <Loader2 className="animate-spin" />} Save and continue
        </Button>
      </Actions>
    </>
  );
}

function SingleStep({ service, settings, onBack, onNext, required }: { service: Service; settings: Settings; onBack: () => void; onNext: () => void; required: string }) {
  const form = useServiceForm(settings, SERVICE_KEYS[service]);
  const tester = useTester(form);
  const { note, saveAndContinue } = useStepSave(form, onNext);
  return (
    <>
      <StepHeading step title="Add a TMDB key">
        TMDB supplies the candidate titles, posters and streaming availability. An account and key are free.
      </StepHeading>
      <ServiceBlock service={service} form={form} tester={tester} />
      <Actions onBack={onBack} note={note}>
        <Button variant="primary" size="lg" disabled={form.saving} onClick={() => void saveAndContinue(() => (hasValue(form, required) ? null : "Paste a TMDB API key to continue."))}>
          {form.saving && <Loader2 className="animate-spin" />} Save and continue
        </Button>
      </Actions>
    </>
  );
}

function HistoryStep({ settings, onBack, onNext }: { settings: Settings; onBack: () => void; onNext: () => void }) {
  const form = useServiceForm(settings, [...SERVICE_KEYS.plex, ...SERVICE_KEYS.jellyfin]);
  const tester = useTester(form);
  const { note, saveAndContinue } = useStepSave(form, onNext);
  const [server, setServer] = useState<"plex" | "jellyfin">(isSet(settings, "jellyfin.url") && !isSet(settings, "plex.url") ? "jellyfin" : "plex");
  return (
    <>
      <StepHeading step title="Add your watch history">
        What you watched, rewatched or gave up on says more about your taste than what you own. You can skip this and add it later.
      </StepHeading>
      <Segmented
        label="Media server"
        value={server}
        onChange={setServer}
        className="mb-5"
        options={[
          { value: "plex", label: "Plex" },
          { value: "jellyfin", label: "Jellyfin" },
        ]}
      />
      <ServiceBlock service={server} form={form} tester={tester} />
      <Actions onBack={onBack} note={note}>
        <Button
          variant="ghost"
          size="lg"
          onClick={() => {
            form.reset();
            onNext();
          }}
        >
          Skip for now
        </Button>
        <Button variant="primary" size="lg" disabled={form.saving || !form.dirty} onClick={() => void saveAndContinue()}>
          {form.saving && <Loader2 className="animate-spin" />} Save and continue
        </Button>
      </Actions>
    </>
  );
}

function ClaudeStep({ settings, onBack, onNext, localLogin }: { settings: Settings; onBack: () => void; onNext: () => void; localLogin: boolean }) {
  const form = useServiceForm(settings, SERVICE_KEYS.claude);
  const tester = useTester(form);
  const { note, saveAndContinue } = useStepSave(form, onNext);
  const hasCredential = hasValue(form, "claude.oauth_token") || hasValue(form, "claude.api_key");
  return (
    <>
      <StepHeading step title="Connect Claude">
        Claude ranks the candidates against your taste and writes the reason for each pick. Use a Claude subscription through a setup token, or an
        Anthropic API key.
      </StepHeading>
      <ServiceBlock service="claude" form={form} tester={tester} />
      <Actions onBack={onBack} note={note}>
        {localLogin && !hasCredential && (
          <Button variant="ghost" size="lg" onClick={onNext}>
            Use the claude login on this machine
          </Button>
        )}
        <Button
          variant="primary"
          size="lg"
          disabled={form.saving}
          onClick={() => void saveAndContinue(() => (hasCredential || localLogin ? null : "Paste a subscription token or an API key to continue."))}
        >
          {form.saving && <Loader2 className="animate-spin" />} Save and continue
        </Button>
      </Actions>
    </>
  );
}

function Done({ settings, missing, onBack }: { settings: Settings; missing: string[]; onBack: () => void }) {
  const navigate = useNavigate();
  const start = useStartRun();
  const kind = isSet(settings, "radarr.url") ? "movies" : "series";
  const connected = (["radarr", "sonarr", "plex", "jellyfin", "tmdb"] as const).filter((s) => isSet(settings, `${s}.url`) || isSet(settings, `${s}.api_key`));
  const claude = isSet(settings, "claude.oauth_token") || isSet(settings, "claude.api_key");

  const runFirst = () =>
    start.mutate(
      { kind, vibe: "" },
      {
        onSuccess: () => navigate(`/?kind=${kind}`),
        onError: (err) => toast.error("Could not start the run", { description: err.message }),
      },
    );

  return (
    <>
      <StepHeading step title={missing.length ? "Almost there" : "Ready for your first picks"}>
        {missing.length
          ? `Proposarr still needs ${missing.join(" and ")} before it can run. Go back to add it, or finish later on the Connections page.`
          : `Your first run reads the ${kind === "movies" ? "Radarr" : "Sonarr"} library and history, then asks Claude. It takes a minute or two.`}
      </StepHeading>
      <ul className="flex flex-wrap gap-2">
        {connected.map((s) => (
          <li key={s} className="inline-flex h-8 items-center gap-1.5 rounded-full border border-success/40 px-3 text-sm">
            <Check className="size-3.5 text-success" /> {SERVICE_META[s].name}
          </li>
        ))}
        {claude && (
          <li className="inline-flex h-8 items-center gap-1.5 rounded-full border border-success/40 px-3 text-sm">
            <Check className="size-3.5 text-success" /> Claude
          </li>
        )}
      </ul>
      <Actions onBack={onBack}>
        <Button asChild variant="ghost" size="lg">
          <Link to={missing.length ? "/connections" : "/"}>{missing.length ? "Open connections" : "Go to picks"}</Link>
        </Button>
        <Button variant={missing.length > 0 ? "secondary" : "primary"} size="lg" disabled={missing.length > 0 || start.isPending} onClick={runFirst}>
          {start.isPending && <Loader2 className="animate-spin" />} Run first recommendations
        </Button>
      </Actions>
    </>
  );
}
