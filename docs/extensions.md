# Building Burrow extensions

Burrow extensions are local folders discovered in the application data directory:

```text
~/Library/Application Support/burrow-wails/extensions/<extension-id>/
```

The **Settings → Extensions** page installs a selected folder or ZIP, enables or
disables extensions, and runs each declared command.

## v1 contract

Each extension has an `extension.json`. Burrow validates the manifest but does
not load third-party code into the desktop process. A command is launched only
when the user clicks it in Settings, runs with the extension directory as its
working directory, and has a 15-second limit.

```json
{
  "apiVersion": 1,
  "id": "hello-burrow",
  "name": "Hello Burrow",
  "version": "0.1.0",
  "description": "The smallest extension.",
  "permissions": ["workspace.read"],
  "commands": [{
    "id": "greet",
    "title": "Say hello",
    "command": "node",
    "args": ["index.mjs"]
  }]
}
```

`id` must be lowercase letters, digits, and hyphens. `command` must name an
executable on `PATH`; it cannot be a path or shell expression. Arguments are
passed directly, not through a shell.

### Host API: command context

This is the complete process-level API available to a command in v1:

| Value | Meaning |
| --- | --- |
| `BURROW_EXTENSION_ID` | Manifest `id` of the running extension. |
| `BURROW_EXTENSION_DIR` | Installed extension directory. This is also the command's working directory. |
| `BURROW_EXTENSION_CWD` | Absolute active-workspace path; empty when no workspace is open. |
| `BURROW_EXTENSION_BRIDGE_URL` + `BURROW_EXTENSION_BRIDGE_TOKEN` | Short-lived, loopback-only capability bridge for this one command run. Use it through `@burrow/sdk`; never log or persist the token. |
| stdout + stderr | Captured together and displayed in Settings after the user runs the command. |
| exit status | A non-zero result is reported as a failed extension command. |

Commands have a 15-second time limit. The host uses an argument vector, not a
shell: `command` is a bare executable found on `PATH`, while `args` is a list of
literal arguments. This intentionally rules out `./script`, absolute command
paths, `sh -c`, pipes, redirection and interpolation.

The `permissions` list is visible to users and forms the future capability
contract. The host bridge currently recognizes `workspace.read`, `secrets.read`,
`secrets.write`, and `tasks.report`; `network.connect` remains disclosure-only
because separately-run processes are not OS-sandboxed. Only install extensions
you trust.

### SDK host capabilities

Use `createHost(process.env)` from `@burrow/sdk` inside a command:

```js
import { createHost } from "@burrow/sdk";

const host = createHost(process.env);
const { cwd } = await host.workspace(); // requires workspace.read
await host.secrets.set("connection", "ssh-host-alias"); // requires secrets.write
const connection = await host.secrets.get("connection"); // requires secrets.read
await host.tasks.report({ id: "sync", title: "Uploading", status: "running", progress: 0.5 });
```

Secrets are namespaced by extension ID and stored in macOS Keychain. Task updates
appear beneath the extension command in Settings. Every bridge request is
authorized by the manifest permissions and a random token valid only while that
one command is running.

### What extensions can build today

The current API is deliberately suited to small, explicit workspace tools:

- Git hygiene and branch-policy checks using `BURROW_EXTENSION_CWD`.
- Project diagnostics such as checking required files, versions, migrations, or generated output.
- Generators that create an artifact only after the user presses their command.
- A native **Workspace Pulse** RP entry, declared in the manifest.

There is deliberately **no** direct API for terminals, chats, arbitrary host
file access, application settings, notifications, custom HTML/CSS/JS panels, or
a reusable local HTTP bridge. An extension must use `@burrow/sdk` rather than
inventing calls such as `window.burrow`.

## Install an extension

Open **Settings → Extensions**, then choose **Choose folder** or **Install ZIP**.
The selected folder must contain `extension.json` at its root. A ZIP may contain
the manifest at its root or inside one top-level folder. Burrow validates the
manifest before copying the extension into its managed extensions folder. If the
selected package uses an already-installed ID, it replaces that extension.

The source-controlled copy is in `examples/extensions/hello-burrow/`.

## Right-panel surfaces

Extensions can add a native surface to the right panel. v1 supports the
host-rendered `workspace-pulse` surface, which reads the active workspace's
branch, working-tree changes, and most recent commit. It uses Burrow's own
components and theme instead of loading extension UI code.

```json
"surfaces": [{
  "id": "workspace-pulse",
  "title": "Workspace Pulse",
  "description": "A compact live summary of this repository.",
  "kind": "workspace-pulse"
}]
```

## Native extension settings

An extension can declare host-rendered text fields. Burrow shows **Configure**
beside that extension in Settings and stores the values outside the extension
folder with user-only file permissions. The command reads a declared value via
the SDK; it cannot read undeclared settings.

```json
"settings": [{
  "id": "host-alias",
  "title": "SSH host alias",
  "description": "Host name from ~/.ssh/config",
  "placeholder": "my-production-server",
  "type": "text",
  "required": true
}]
```

Declare `settings.read`, then access it while the command runs:

```js
const alias = await createHost(process.env).settings.get("host-alias");
```

This is intentionally a native, declarative form rather than extension-provided
HTML. SFTP Sync uses it for the SSH alias and remote directory, so users do not
need to edit a connection JSON file.

## Next API versions

Future versions can add declarative panels, agent/provider adapters, and
capability-scoped calls into Burrow's Control API. Keep an extension's logic in
its own process so an extension crash cannot destabilize Burrow.

## A useful command pattern

For a predictable project check, keep the script inside the extension directory
and use the workspace context only after checking it exists:

```js
import { existsSync } from "node:fs";
import { join } from "node:path";

const cwd = process.env.BURROW_EXTENSION_CWD;
if (!cwd) {
  console.error("Open a workspace before running this check.");
  process.exit(1);
}

const required = ["package.json", "README.md"];
const missing = required.filter((file) => !existsSync(join(cwd, file)));
console.log(missing.length ? `Missing: ${missing.join(", ")}` : "Workspace looks ready.");
process.exit(missing.length ? 1 : 0);
```

Declare it without a shell:

```json
"commands": [{
  "id": "check-project",
  "title": "Check project",
  "command": "node",
  "args": ["check-project.mjs"]
}]
```

## Writing extensions with an LLM

Use the copy-ready prompt in `docs/extensions-for-ai.md`. It gives an AI the
exact manifest, host context, safety boundaries and required deliverables, so it
will not invent unsupported host APIs.

## SDK (first step)

`packages/sdk` contains the initial `@burrow/sdk` authoring package. It offers
typed manifest definitions, local validation, and helpers for reading the v1
command context. It deliberately does not claim to expose hidden application
APIs. The SFTP starter in `examples/extensions/sftp-sync/` shows the intended
authoring shape while secure credentials, capability-scoped networking, progress
tasks and richer native surface primitives are designed for the next SDK step.
