// Fades out and removes index.html's #splash. Idempotent.
export function hideSplash() {
  const el = document.getElementById("splash");
  if (!el) return;
  el.style.opacity = "0";
  el.style.pointerEvents = "none";
  setTimeout(() => el.remove(), 400);
}
