// A copy-ready contract for LLMs. Keep it aligned with docs/extensions-for-ai.md.
export const extensionAuthoringPrompt = `Create a Burrow extension using extension API v1.

Deliver a folder whose root contains extension.json. Do not use an iframe, webview, HTTP server, browser automation, or a shell command string.

Manifest requirements:
- apiVersion is 1.
- id matches ^[a-z0-9][a-z0-9-]{1,62}$.
- name and version are non-empty.
- Each command has id, title and command. command is a bare executable on PATH (such as node, python3, git, rg); never ./script, /path/to/script, sh -c, pipes, redirects, or shell syntax.
- args is an array of literal arguments. A command runs from the extension folder and has a 15 second time limit.
- An optional surface can only use kind "workspace-pulse". It is a native host-rendered RP surface; extensions cannot supply HTML, CSS or JavaScript for it.
- Optional settings are host-rendered text fields. Each has id, title, type: "text", optional description/placeholder, and optional required. A command reads declared fields with createHost(process.env).settings.get(id) and must declare settings.read.

Runtime context:
- BURROW_EXTENSION_ID: manifest id.
- BURROW_EXTENSION_DIR: installed extension folder.
- BURROW_EXTENSION_CWD: active workspace path, or empty when none is active.
- stdout and stderr are shown after the user explicitly presses the command button in Settings → Extensions.

Safety and scope:
- Permissions are informational in v1, not an OS sandbox.
- BURROW_EXTENSION_BRIDGE_URL and BURROW_EXTENSION_BRIDGE_TOKEN: use only through createHost(process.env) from @burrow/sdk; valid for one command run.
- The SDK host offers workspace() with workspace.read, Keychain secrets with secrets.read/secrets.write, and tasks.report() with tasks.report. Network is disclosed through network.connect but is not OS-sandboxed.
- v1 has no direct API for tabs, terminals, arbitrary host files, settings, notifications, or custom right-panel UI. Do not invent calls such as window.burrow or a reusable localhost service.
- Handle an empty BURROW_EXTENSION_CWD cleanly.

Produce extension.json, the executable script, and a concise README with install steps: Settings → Extensions → Choose folder or Install ZIP. Prefer one useful deterministic task such as repository hygiene, branch checks, project diagnostics, changelog preparation, or a code generator.`;
