import { defineExtension } from "@burrow/sdk";

// Author this as TypeScript, then generate extension.json during packaging.
// Network and stored credentials are deliberately not available in v1 yet.
export default defineExtension({
  manifest: {
    apiVersion: 1,
    id: "sftp-sync",
    name: "SFTP Sync",
    version: "0.1.0",
    description: "Starter declaration for a future capability-scoped SFTP extension.",
    permissions: ["workspace.read", "network.connect"],
    surfaces: [{
      id: "workspace-pulse",
      title: "SFTP Sync",
      description: "Workspace status while SFTP capabilities are being added.",
      kind: "workspace-pulse",
    }],
  },
});
