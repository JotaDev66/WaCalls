import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath } from "node:url";

export default defineConfig(({ mode }) => {
  // Load .env (all keys, not just VITE_*) for dev-server config.
  const env = loadEnv(mode, process.cwd(), "");
  const port = Number(env.PORT ?? 5173);
  const apiTarget = env.VITE_API_TARGET || `http://localhost:${env.API_PORT ?? 8080}`;

  return {
    plugins: [react()],
    resolve: {
      alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) },
    },
    server: {
      port,
      proxy: {
        "/api": { target: apiTarget, changeOrigin: true, ws: false },
      },
    },
  };
});
