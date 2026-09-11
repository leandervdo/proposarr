import { fileURLToPath, URL } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
  },
  build: {
    emptyOutDir: true,
    chunkSizeWarningLimit: 900,
  },
  server: {
    proxy: {
      "/api": {
        target: "http://localhost:8585",
        changeOrigin: true,
        // Keep server-sent events streaming through the proxy.
        configure: (proxy) => {
          proxy.on("proxyRes", (res) => {
            if (res.headers["content-type"]?.includes("text/event-stream")) {
              res.headers["cache-control"] = "no-cache";
              res.headers["x-accel-buffering"] = "no";
            }
          });
        },
      },
      "/healthz": "http://localhost:8585",
    },
  },
});
