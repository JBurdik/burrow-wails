// Which pre-release channel this build is, or null on stable. Drives the
// titlebar badge, the sidebar artwork (StageBackdrop) and whether Settings
// offers the "Release channel marker" choice at all.
export const releaseChannel: "dev" | "beta" | null = import.meta.env.DEV
  ? "dev"
  : import.meta.env.VITE_APP_CHANNEL === "beta" ? "beta" : null;
