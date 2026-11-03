import type { Component } from "vue";
import { PhRobot, PhSparkle, PhTerminal, PhCode, PhStarFour, PhBrain, PhGoogleLogo } from "@phosphor-icons/vue";
import ClaudeIcon from "@/components/icons/ClaudeIcon.vue";
import OpenAIIcon from "@/components/icons/OpenAIIcon.vue";
import GitHubCopilotIcon from "@/components/icons/GitHubCopilotIcon.vue";
import MistralIcon from "@/components/icons/MistralIcon.vue";

// Icon keys a chat agent can use, mapped to their render component.
export const AGENT_ICONS: Record<string, Component> = {
  claude: ClaudeIcon,
  openai: OpenAIIcon,
  copilot: GitHubCopilotIcon,
  gemini: PhGoogleLogo,
  mistral: MistralIcon,
  robot: PhRobot,
  sparkle: PhSparkle,
  star: PhStarFour,
  brain: PhBrain,
  terminal: PhTerminal,
  code: PhCode,
};

export const AGENT_ICON_KEYS = Object.keys(AGENT_ICONS);

export function agentIconComp(key?: string): Component {
  return AGENT_ICONS[key ?? ""] ?? PhRobot;
}
