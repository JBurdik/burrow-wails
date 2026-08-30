import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";
import tailwindcss from "@tailwindcss/vite";
import { resolve } from "path";

const isMobileBuild = process.env.VITE_TARGET === "mobile";

export default defineConfig(async () => ({
  plugins: [vue(), tailwindcss()],
  // The mobile bundle is served both from the local root (`http://127.0.0.1:8420/`)
  // and through Tailscale Serve at `/burrow/`. Relative asset URLs work in both
  // places and do not accidentally load another app's `/assets` bundle.
  base: isMobileBuild ? "./" : "/",
  resolve: {
    alias: {
      "@": resolve(__dirname, "src"),
      // Go+Wails backend rewrite (rewrite/go-wails branch): route the
      // @tauri-apps/api imports Vue components still use to compat shims
      // backed by the Wails-generated Go bindings, instead of touching
      // every call-site across src/. See src/lib/wailsCompat/.
      "@tauri-apps/api/core": resolve(__dirname, "src/lib/wailsCompat/core.ts"),
      "@tauri-apps/api/event": resolve(__dirname, "src/lib/wailsCompat/event.ts"),
      "@tauri-apps/api/webview": resolve(__dirname, "src/lib/wailsCompat/webview.ts"),
      "@tauri-apps/api/webviewWindow": resolve(__dirname, "src/lib/wailsCompat/webviewWindow.ts"),
      "@tauri-apps/plugin-dialog": resolve(__dirname, "src/lib/wailsCompat/dialog.ts"),
      "@tauri-apps/plugin-notification": resolve(__dirname, "src/lib/wailsCompat/notification.ts"),
      "@tauri-apps/plugin-shell": resolve(__dirname, "src/lib/wailsCompat/shell.ts"),
      "@tauri-apps/plugin-updater": resolve(__dirname, "src/lib/wailsCompat/updater.ts"),
      "@tauri-apps/plugin-process": resolve(__dirname, "src/lib/wailsCompat/process.ts"),
      "@tauri-apps/api/window": resolve(__dirname, "src/lib/wailsCompat/window.ts"),
      "@tauri-apps/api/app": resolve(__dirname, "src/lib/wailsCompat/app.ts"),
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
    // The mobile bundle is //go:embed-ed by src-wails/httpserver.go, so it
    // has to land inside the Go module dir (embed cannot reach ../). The
    // extra app/ level keeps the committed .gitkeep out of reach of
    // emptyOutDir — go:embed needs the dir non-empty to compile.
    emptyOutDir: true,
    outDir: isMobileBuild ? "src-wails/dist-mobile/app" : "dist",
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
