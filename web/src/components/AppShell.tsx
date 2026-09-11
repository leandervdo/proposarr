import { Cable, History, Library, Moon, Popcorn, SlidersHorizontal, Sun } from "lucide-react";
import { useEffect } from "react";
import { NavLink, Outlet, useLocation, useNavigate } from "react-router";
import { toast, Toaster } from "sonner";
import { useLive } from "@/api/live";
import { useStatus } from "@/api/queries";
import { isUnreachable } from "@/api/client";
import { duration, kindLabel } from "@/lib/format";
import { useTheme } from "@/lib/theme";
import { useTick } from "@/lib/useTick";
import { cn } from "@/lib/utils";
import { BackendDown } from "./EmptyState";
import { Tooltip } from "./ui/tooltip";

const NAV = [
  { to: "/", label: "Picks", icon: Popcorn, end: true },
  { to: "/runs", label: "Runs", icon: History },
  { to: "/library", label: "Library", icon: Library },
  { to: "/connections", label: "Connections", icon: Cable },
  { to: "/settings", label: "Settings", icon: SlidersHorizontal },
];

export function AppShell() {
  const [theme, toggleTheme] = useTheme();
  const status = useStatus();
  const { pathname } = useLocation();

  useEffect(() => {
    window.scrollTo({ top: 0 });
  }, [pathname]);

  const { lastFinished } = useLive();
  const navigate = useNavigate();
  useEffect(() => {
    if (!lastFinished) return;
    const noun = lastFinished.kind === "series" ? "series" : "movie";
    if (lastFinished.status === "succeeded") {
      toast.success(`New ${noun} picks are ready`, {
        description: `${lastFinished.pick_count} picks${lastFinished.vibe ? ` for “${lastFinished.vibe}”` : ""}`,
        action: { label: "Show", onClick: () => navigate(`/?kind=${lastFinished.kind}`) },
      });
    } else if (lastFinished.status === "rate_limited") {
      toast.warning("Claude session limit reached", { description: "Run again after your subscription window resets." });
    } else if (lastFinished.status === "failed") {
      toast.error(`The ${noun} run failed`, { description: lastFinished.error });
    }
  }, [lastFinished, navigate]);

  const down = status.isError && isUnreachable(status.error);

  return (
    <div className="min-h-dvh bg-background text-text lg:grid lg:grid-cols-[15rem_1fr]">
      <a href="#main" className="sr-only focus:not-sr-only focus:fixed focus:top-3 focus:left-3 focus:z-[60] focus:rounded-md focus:bg-accent focus:px-3 focus:py-2 focus:text-accent-contrast">
        Skip to content
      </a>

      {/* Desktop sidebar */}
      <aside className="sticky top-0 hidden h-dvh flex-col border-r border-border px-4 pt-7 pb-5 lg:flex">
        <Wordmark />
        <nav aria-label="Main" className="mt-9 flex flex-col gap-0.5">
          {NAV.map(({ to, label, icon: Icon, end }) => (
            <NavLink
              key={to}
              to={to}
              end={end}
              className={({ isActive }) =>
                cn(
                  "group flex h-10 items-center gap-3 rounded-[var(--radius-control)] px-3 text-[15px] transition-colors",
                  isActive ? "bg-surface-raised font-semibold text-text" : "text-text-muted hover:bg-surface hover:text-text",
                )
              }
            >
              {({ isActive }) => (
                <>
                  <Icon className={cn("size-[18px]", isActive && "text-accent")} strokeWidth={isActive ? 2.25 : 1.75} />
                  {label}
                </>
              )}
            </NavLink>
          ))}
        </nav>
        <div className="mt-auto flex flex-col gap-3">
          <LiveRuns />
          <div className="flex items-center justify-between px-1 text-xs text-text-muted">
            <span className="nums">{status.data ? `Version ${status.data.version}` : ""}</span>
            <ThemeButton theme={theme} onToggle={toggleTheme} />
          </div>
        </div>
      </aside>

      {/* Mobile top bar */}
      <header className="sticky top-0 z-30 flex h-14 items-center justify-between border-b border-border bg-background/85 px-4 backdrop-blur-lg lg:hidden">
        <Wordmark compact />
        <div className="flex items-center gap-1">
          <LiveDot />
          <ThemeButton theme={theme} onToggle={toggleTheme} />
        </div>
      </header>

      <main id="main" className="min-w-0 px-4 pt-6 pb-28 sm:px-6 lg:px-10 lg:pt-10 lg:pb-16">
        <div className="mx-auto w-full max-w-[96rem]">{down ? <BackendDown onRetry={() => void status.refetch()} /> : <Outlet />}</div>
      </main>

      {/* Mobile bottom navigation */}
      <nav
        aria-label="Main"
        className="fixed inset-x-0 bottom-0 z-30 grid grid-cols-5 border-t border-border bg-background/90 pb-[env(safe-area-inset-bottom)] backdrop-blur-lg lg:hidden"
      >
        {NAV.map(({ to, label, icon: Icon, end }) => (
          <NavLink
            key={to}
            to={to}
            end={end}
            className={({ isActive }) =>
              cn("flex h-16 flex-col items-center justify-center gap-1 text-[11px] font-medium", isActive ? "text-text" : "text-text-muted")
            }
          >
            {({ isActive }) => (
              <>
                <Icon className={cn("size-5", isActive && "text-accent")} strokeWidth={isActive ? 2.25 : 1.75} />
                {label}
              </>
            )}
          </NavLink>
        ))}
      </nav>

      <Toaster
        theme={theme}
        position="bottom-right"
        mobileOffset={{ bottom: 84 }}
        toastOptions={{
          classNames: {
            toast: "!bg-surface-raised !text-text !border-border !rounded-[var(--radius-control)] !font-sans !shadow-[0_16px_48px_-12px_rgb(0_0_0/0.5)]",
            description: "!text-text-muted",
            actionButton: "!bg-accent !text-accent-contrast !font-semibold",
          },
        }}
      />
    </div>
  );
}

function Wordmark({ compact }: { compact?: boolean }) {
  return (
    <NavLink to="/" className="group flex items-baseline gap-2 rounded-sm px-1" aria-label="Proposarr, go to picks">
      <span className={cn("font-display leading-none font-bold tracking-tight", compact ? "text-[26px]" : "text-[34px]")}>
        Propos<span className="text-accent">arr</span>
      </span>
    </NavLink>
  );
}

function ThemeButton({ theme, onToggle }: { theme: "dark" | "light"; onToggle: () => void }) {
  const next = theme === "dark" ? "light" : "dark";
  return (
    <Tooltip content={`Switch to ${next} theme`}>
      <button
        type="button"
        onClick={onToggle}
        aria-label={`Switch to ${next} theme`}
        className="grid size-9 place-items-center rounded-full text-text-muted transition-colors hover:bg-surface-raised hover:text-text"
      >
        {theme === "dark" ? <Sun className="size-[18px]" /> : <Moon className="size-[18px]" />}
      </button>
    </Tooltip>
  );
}

function LiveRuns() {
  const { running } = useLive();
  const list = Object.values(running);
  useTick(list.length > 0);
  if (list.length === 0) return null;
  return (
    <div className="flex flex-col gap-2" aria-live="polite">
      {list.map((r) => (
        <NavLink key={r.runId} to="/runs" className="rounded-[var(--radius-control)] border border-accent/35 bg-accent-soft/60 p-3 transition-colors hover:border-accent/70">
          <div className="flex items-center gap-2 text-[13px] font-semibold">
            <span className="size-2 animate-pulse-dot rounded-full bg-accent" />
            {kindLabel(r.kind)} run
            {r.startedAt && <span className="nums ml-auto font-normal text-text-muted">{duration(r.startedAt)}</span>}
          </div>
          <p className="mt-1 line-clamp-2 text-[13px] text-text-muted">{r.message}</p>
        </NavLink>
      ))}
    </div>
  );
}

function LiveDot() {
  const { running } = useLive();
  const list = Object.values(running);
  if (list.length === 0) return null;
  return (
    <NavLink to="/runs" className="flex h-9 items-center gap-2 rounded-full px-3 text-[13px] text-text-muted hover:bg-surface-raised" aria-live="polite">
      <span className="size-2 animate-pulse-dot rounded-full bg-accent" />
      <span className="max-w-[9rem] truncate">{list[0]!.message}</span>
    </NavLink>
  );
}
