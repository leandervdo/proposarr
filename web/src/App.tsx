import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { MotionConfig } from "motion/react";
import { createBrowserRouter, RouterProvider } from "react-router";
import { ApiError } from "./api/client";
import { LiveProvider } from "./api/live";
import { AppShell } from "./components/AppShell";
import { TooltipProvider } from "./components/ui/tooltip";
import { CollectionPage } from "./pages/CollectionPage";
import { ConnectionsPage } from "./pages/ConnectionsPage";
import { LibraryPage } from "./pages/LibraryPage";
import { NotFoundPage } from "./pages/NotFoundPage";
import { PicksPage } from "./pages/PicksPage";
import { RunsPage } from "./pages/RunsPage";
import { SettingsPage } from "./pages/SettingsPage";
import { SetupPage } from "./pages/SetupPage";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      // Client errors will not fix themselves; everything else gets two retries.
      retry: (count, err) => !(err instanceof ApiError && err.status >= 400 && err.status < 500) && count < 2,
    },
  },
});

// A data router, so pages with unsaved edits can block navigation.
const router = createBrowserRouter([
  { path: "/setup", element: <SetupPage /> },
  {
    element: <AppShell />,
    children: [
      { index: true, element: <PicksPage /> },
      { path: "collection", element: <CollectionPage /> },
      { path: "runs", element: <RunsPage /> },
      { path: "library", element: <LibraryPage /> },
      { path: "connections", element: <ConnectionsPage /> },
      { path: "settings", element: <SettingsPage /> },
      { path: "*", element: <NotFoundPage /> },
    ],
  },
]);

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <LiveProvider>
        <MotionConfig reducedMotion="user">
          <TooltipProvider delayDuration={300}>
            <RouterProvider router={router} />
          </TooltipProvider>
        </MotionConfig>
      </LiveProvider>
    </QueryClientProvider>
  );
}
