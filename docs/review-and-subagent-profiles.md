# Review comments and sub-agent profiles

## Resolve and reopen

Comments on agent responses and diff lines have a separate resolved state.
Use **Resolve** when a finding is addressed, and **Reopen** when it needs more
work. The original quote and send receipt remain available. Reopening a sent
comment does not send it a second time; write a new follow-up to request more
work. An unsent resolved comment is excluded from the pending batch.

The diff's **Review comments** list includes notes whose original line or file
is no longer in the displayed patch. Their status can still be changed there.
Diff review state is stored in SQLite. Response comments remain stored on this
device alongside their quotes and follow-up references.

## Saved roles

Open **Settings → Sub-agent profiles** to edit Scout, Worker, and Reviewer.
Each role has instructions, a provider, an optional model, and a permission
mode. Scout and Reviewer initially use Plan; Worker initially uses Supervised.
Changes affect future delegations, not agents already running.

Select a role in the sub-agent spawn form, or use:

```sh
burrow spawn "Investigate the failing login test" --profile scout
burrow spawn "Implement the selected fix" --profile worker
burrow spawn "Review the current changes" --profile reviewer
```

`burrow list-subagent-profiles` exposes the saved settings to an agent. Explicit
`--agent` and `--model` arguments override the role's provider and model. Choose
**No profile**, or omit `--profile`, for the existing delegation behavior.

Profiles currently support native Claude CLI and Codex app-server providers.
The same role prompt, model, and permissions are applied to chat and terminal
launches. Unsupported providers produce a visible error rather than launching
with a different permission mode. Chat settings are saved before the child
becomes visible; Codex restores its mode and model before sending the first
prompt. These settings belong to the child and do not change global defaults.
Terminal profiles preserve the provider environment and Claude configuration
directory. Providers with custom CLI arguments must use chat or a separate
provider without those arguments; the terminal launch reports an error instead
of allowing extra flags to override the role's permissions.
