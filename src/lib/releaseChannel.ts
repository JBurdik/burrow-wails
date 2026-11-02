// Which pre-release channel this build is, or null on stable. Drives the
// titlebar badge, the sidebar artwork (StageBackdrop) and whether Settings
// offers the "Release channel marker" choice at all.
//
// The phone bundle is always a production build embedded in the Go binary, so
// there the SERVING binary decides: httpserver.go stamps `data-channel` on
// mobile.html (src-wails/channel.go).
const served = typeof document === "undefined" ? undefined : document.documentElement.dataset.channel;
export const releaseChannel: "dev" | "beta" | null = import.meta.env.DEV
  ? "dev"
  : import.meta.env.VITE_APP_CHANNEL === "beta" || served === "beta" ? "beta"
  : served === "dev" ? "dev" : null;
