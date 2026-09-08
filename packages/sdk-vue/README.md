# @burrow/sdk-vue

Vue-friendly declarations for a **native Burrow surface**. This package does not create an iframe, inject HTML, or let an extension run arbitrary DOM in the right panel.

```ts
import { Action, ActionPanel, List, ListItem, defineSurface } from "@burrow/sdk-vue";

export const deployments = defineSurface({
  id: "deployments",
  title: "Deployments",
  root: List({
    title: "Recent deployments",
    children: [
      ListItem({
        id: "api-prod",
        title: "API · production",
        subtitle: "Healthy",
        actions: ActionPanel({ children: [Action({ id: "open-logs", title: "Open logs" })] }),
      }),
    ],
  }),
});
```

The initial vocabulary is `List`, `ListItem`, `Detail`, `Form`, `TextField`, `Section`, `ActionPanel`, and `Action`. All values are JSON-safe. Actions use IDs so the extension receives an explicit event rather than exporting a callback into Burrow.

## Vue integration

The factories work naturally in `computed()` or a Composition API `render()` function; use the current value when publishing a surface through the extension host. A full Vue custom renderer / SFC runtime is intentionally not part of v0.1 yet: it needs a persistent, lifecycle-managed extension process and event channel. Until then the SDK guarantees only this safe data boundary.
