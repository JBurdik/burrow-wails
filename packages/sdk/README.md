# @burrow/sdk

The typed authoring and host-capability layer for Burrow extension API v1. It
validates the same manifest constraints as the host and exposes the explicit
workspace context given to command processes.

```ts
import { defineExtension } from "@burrow/sdk";

export default defineExtension({
  manifest: {
    apiVersion: 1,
    id: "sftp-sync",
    name: "SFTP Sync",
    version: "0.1.0",
    permissions: ["workspace.read", "network.connect"],
    commands: [{
      id: "check-workspace",
      title: "Check workspace",
      command: "node",
      args: ["check-workspace.mjs"]
    }],
    surfaces: [{ id: "workspace-pulse", title: "SFTP Sync", kind: "workspace-pulse" }]
  }
});
```

`createHost(process.env)` provides capability-scoped workspace access, macOS
Keychain secrets, and task progress reporting through a short-lived loopback
bridge. It does not permit custom UI, terminal control, or arbitrary host file
access. The complete contract is documented in `docs/extensions.md`.
