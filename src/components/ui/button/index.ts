import { type VariantProps, cva } from "class-variance-authority";

export { default as Button } from "./Button.vue";

// T3 Code look: sharp-ish corners (rounded-md, not pill), thin borders,
// pink/magenta primary — no heavy shadows.
export const buttonVariants = cva(
  "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0",
  {
    variants: {
      variant: {
        default: "bg-accent text-white hover:bg-accent-dim",
        secondary: "bg-hover text-foreground border border-border hover:bg-selected",
        outline: "border border-border bg-transparent text-foreground hover:bg-hover",
        ghost: "text-foreground hover:bg-hover",
        destructive: "bg-destructive text-white hover:opacity-90",
        link: "text-accent underline-offset-4 hover:underline",
      },
      size: {
        default: "h-8 px-3",
        sm: "h-7 px-2.5 text-xs",
        lg: "h-10 px-5",
        icon: "h-8 w-8",
      },
    },
    defaultVariants: { variant: "default", size: "default" },
  },
);

export type ButtonVariants = VariantProps<typeof buttonVariants>;
