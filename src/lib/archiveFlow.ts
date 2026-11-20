// Archive flow (threadbear's inbox zero): the Undo notice over the thread area
// and the "all caught up" line on the welcome surface. Sidebar writes both;
// ArchiveUndo.vue / WelcomeScreen.vue read them.
import { ref, shallowRef } from "vue";

export interface ArchiveNotice { title: string; undo: () => void }

/** A new object restarts the notice's timer. */
export const archiveNotice = shallowRef<ArchiveNotice | null>(null);
/** Set when archiving the open thread left nothing in the Inbox; cleared when the welcome surface goes. */
export const inboxZero = ref(false);
