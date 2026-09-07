/**
 * A serialisable UI vocabulary rendered by Burrow, not by an extension webview.
 *
 * These factories are intentionally usable from Vue's Composition API: create a
 * tree in a computed() or a render() function and send its current value to the
 * host. Events are named actions, never executable JavaScript crossing the
 * extension boundary.
 */
export type SurfaceNode =
  | ListNode
  | ListItemNode
  | DetailNode
  | FormNode
  | TextFieldNode
  | SectionNode
  | ActionPanelNode
  | ActionNode;

export interface BaseNode {
  type: SurfaceNode["type"];
}

export interface ListNode extends BaseNode {
  type: "list";
  title?: string;
  searchPlaceholder?: string;
  children: Array<ListItemNode | SectionNode>;
}

export interface ListItemNode extends BaseNode {
  type: "list-item";
  id: string;
  title: string;
  subtitle?: string;
  accessories?: string[];
  actions?: ActionPanelNode;
}

export interface DetailNode extends BaseNode {
  type: "detail";
  title: string;
  markdown: string;
  actions?: ActionPanelNode;
}

export interface FormNode extends BaseNode {
  type: "form";
  title: string;
  submitAction: string;
  children: Array<TextFieldNode | SectionNode>;
}

export interface TextFieldNode extends BaseNode {
  type: "text-field";
  id: string;
  label: string;
  value?: string;
  placeholder?: string;
  required?: boolean;
}

export interface SectionNode extends BaseNode {
  type: "section";
  title?: string;
  children: Array<ListItemNode | TextFieldNode>;
}

export interface ActionPanelNode extends BaseNode {
  type: "action-panel";
  children: ActionNode[];
}

export interface ActionNode extends BaseNode {
  type: "action";
  id: string;
  title: string;
  style?: "default" | "destructive";
}

export interface SurfaceDefinition {
  id: string;
  title: string;
  root: ListNode | DetailNode | FormNode;
}

function node<T extends SurfaceNode>(value: T): T {
  return Object.freeze(value);
}

/** Native list, comparable to Raycast's List but rendered by Burrow. */
export const List = (options: Omit<ListNode, "type">): ListNode => node({ type: "list", ...options });
export const ListItem = (options: Omit<ListItemNode, "type">): ListItemNode => node({ type: "list-item", ...options });
export const Detail = (options: Omit<DetailNode, "type">): DetailNode => node({ type: "detail", ...options });
export const Form = (options: Omit<FormNode, "type">): FormNode => node({ type: "form", ...options });
export const TextField = (options: Omit<TextFieldNode, "type">): TextFieldNode => node({ type: "text-field", ...options });
export const Section = (options: Omit<SectionNode, "type">): SectionNode => node({ type: "section", ...options });
export const ActionPanel = (options: Omit<ActionPanelNode, "type">): ActionPanelNode => node({ type: "action-panel", ...options });
export const Action = (options: Omit<ActionNode, "type">): ActionNode => node({ type: "action", ...options });

/**
 * Makes a native surface payload. It contains data only and is safe to pass
 * through the Burrow bridge. Action ids are delivered back to the extension;
 * callbacks and arbitrary Vue DOM are deliberately not serialised.
 */
export function defineSurface(definition: SurfaceDefinition): Readonly<SurfaceDefinition> {
  if (!definition.id.trim() || !definition.title.trim()) throw new Error("A surface needs an id and title");
  validateNode(definition.root);
  return Object.freeze({ ...definition });
}

function validateNode(value: SurfaceNode): void {
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new Error("Surface nodes must be objects");
  if (Object.values(value).some((entry) => typeof entry === "function")) {
    throw new Error("Surface nodes cannot contain callbacks; use a named Action instead");
  }
  switch (value.type) {
    case "list":
    case "form":
    case "section":
    case "action-panel":
      value.children.forEach(validateNode);
      break;
    case "list-item":
    case "detail":
      if (value.actions) validateNode(value.actions);
      break;
  }
}
