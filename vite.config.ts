import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import { resolve } from "path";

const isMobileBuild = process.env.VITE_TARGET === "mobile";

export default defineConfig(async () => ({
  plugins: [vue()],
  // The mobile bundle is served both from the local root (`http://127.0.0.1:8420/`)
  // and through Tailscale Serve at `/burrow/`. Relative asset URLs work in both
  // places and do not accidentally load another app's `/assets` bundle.
  base: isMobileBuild ? "./" : "/",
  resolve: {
    alias: {
      "@": resolve(__dirname, "src"),
    },
  },
  build: {
    // Rollup scope-hoisting + esbuild minification produced a broken `i` variable
    // reference (TDZ/collision) that threw `ReferenceError: Can't find variable: i`
    // mid-stream while xterm parsed an agent's terminal capability queries. That
    // aborted parsing, so query-driven TUIs (GitHub Copilot, opencode) never got
    // their responses and hung on a blank alt-screen — only in the minified prod
    // bundle; the dev server (unminified) was fine. Disabling minification is the
    // safe fix for a desktop app where the JS loads from local disk (size is moot).
    minify: false,
    outDir: isMobileBuild ? "dist-mobile" : "dist",
    rollupOptions: {
      input: isMobileBuild
        ? { mobile: resolve(__dirname, "mobile.html") }
        : { main: resolve(__dirname, "index.html") },
    },
  },
  clearScreen: false,
  server: {
    port: 1420,
    strictPort: true,
    watch: {
      ignored: ["**/src-tauri/**"],
    },
  },
}));
