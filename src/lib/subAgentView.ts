// Which sub-agent chat, if any, the Right Panel currently wants to show.
//
// A module-level ref rather than Pinia state: it is view routing for exactly
// one component (`SubAgentHost.vue`'s `<Teleport>` target), not app state
// anything else needs to read, persist or react to across a reload. The Right
// Panel (task 9) sets it; SubAgentHost only reads it to decide which mounted
// child, if any, to teleport into its `#subagent-slot`.
import { ref } from "vue";

/** Chat id of the sub-agent the panel wants on screen, or `null` when none is
 *  open. Every other mounted child stays hidden (`v-show="false"`) rather
 *  than unmounted — see SubAgentHost.vue for why. */
export const subAgentViewTarget = ref<number | null>(null);
