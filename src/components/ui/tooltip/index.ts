// Thin re-export of reka-ui's Tooltip primitives, pre-styled at the call
// site (TooltipContent) rather than wrapped — reka-ui's own Root/Trigger
// need no styling, only Content does.
export { TooltipProvider, TooltipRoot, TooltipTrigger } from "reka-ui";
export { default as TooltipContent } from "./TooltipContent.vue";
