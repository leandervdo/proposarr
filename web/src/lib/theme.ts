import { useCallback, useState } from "react";

export type Theme = "dark" | "light";
const KEY = "proposarr.theme";

function current(): Theme {
  return document.documentElement.classList.contains("dark") ? "dark" : "light";
}

export function useTheme(): [Theme, () => void] {
  const [theme, setTheme] = useState<Theme>(current);
  const toggle = useCallback(() => {
    const next: Theme = current() === "dark" ? "light" : "dark";
    document.documentElement.classList.toggle("dark", next === "dark");
    document.querySelector('meta[name="theme-color"]')?.setAttribute("content", next === "dark" ? "#15141a" : "#f2f1f4");
    try {
      localStorage.setItem(KEY, next);
    } catch {
      // Private mode; the choice lasts for this page only.
    }
    setTheme(next);
  }, []);
  return [theme, toggle];
}
