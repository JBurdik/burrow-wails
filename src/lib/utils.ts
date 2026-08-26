import { type ClassValue, clsx } from "clsx";
import { twMerge } from "tailwind-merge";

// Standard shadcn-vue helper: merges Tailwind classes, later ones win
// (e.g. a consumer's `class="p-4"` overriding a component's own `p-2`).
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
