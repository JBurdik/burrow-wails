import { ref } from "vue";
import { isDesktop } from "@/lib/wailsCompat/desktop";

/** No Wails runtime: a browser driving a paired Burrow (iPad, phone, `pnpm dev`). */
export const isWeb = !isDesktop();

const mq = typeof window !== "undefined" && window.matchMedia ? window.matchMedia("(max-width: 767px)") : null;

/** Phone width: one surface at a time, the sidebar as a drawer (App.vue). */
export const compact = ref(mq?.matches ?? false);
mq?.addEventListener("change", (e) => (compact.value = e.matches));
