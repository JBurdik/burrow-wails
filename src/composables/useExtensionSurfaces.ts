import { computed, ref } from "vue";
import { invoke } from "@tauri-apps/api/core";
import { PhPulse } from "@phosphor-icons/vue";

export interface ExtensionSurface {
  id: string;
  title: string;
  description: string;
  kind: "workspace-pulse";
}

interface ExtensionInfo {
  id: string;
  enabled: boolean;
  error?: string;
  surfaces?: ExtensionSurface[];
}

export interface RegisteredExtensionSurface extends ExtensionSurface {
  tabId: string;
  extensionId: string;
  label: string;
  icon: typeof PhPulse;
}

export function useExtensionSurfaces() {
  const extensions = ref<ExtensionInfo[]>([]);
  const loadError = ref("");

  const surfaces = computed<RegisteredExtensionSurface[]>(() =>
    extensions.value.flatMap((extension) =>
      extension.enabled && !extension.error
        ? (extension.surfaces ?? []).map((surface) => ({
          ...surface,
          extensionId: extension.id,
          tabId: `extension:${extension.id}:${surface.id}`,
          label: surface.title,
          icon: PhPulse,
        }))
        : [],
    ),
  );

  async function load() {
    try {
      extensions.value = await invoke<ExtensionInfo[]>("list_extensions");
      loadError.value = "";
    } catch (error) {
      loadError.value = String(error);
    }
  }

  return { surfaces, load, loadError };
}
