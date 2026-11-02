// Images in a transcript page arrive as stubs ("burrow-image://<index>/<n>":
// image n of the message at transcript index <index>) and are fetched with
// `load_chat_image` when the user opens one. Mirrors chatImageStubPrefix in
// src-wails/chatstore.go.
const PREFIX = "burrow-image://";

export const isImageStub = (src: string): boolean => src.startsWith(PREFIX);

export function parseImageStub(src: string): { index: number; image: number } | null {
  if (!isImageStub(src)) return null;
  const [index, image] = src.slice(PREFIX.length).split("/").map(Number);
  return Number.isInteger(index) && Number.isInteger(image) ? { index, image } : null;
}
