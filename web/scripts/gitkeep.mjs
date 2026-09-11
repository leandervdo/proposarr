// Vite empties dist/ on build; the Go embed needs dist/.gitkeep to exist.
import { mkdirSync, writeFileSync } from "node:fs";

mkdirSync("dist", { recursive: true });
writeFileSync("dist/.gitkeep", "");
