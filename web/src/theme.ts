// Picks the skin (a set of semantic tokens in styles.css) for the host.
// Inside the CPA WebUI the page is a same-origin iframe: follow the parent's
// <html data-theme> (absent = warm gray, "white", "dark") and watch it.
// Anywhere else (EasyCLI WebView, opened directly) follow the system scheme
// with the EasyCLI light/dark palette.

export type Skin = "easycli-light" | "easycli-dark" | "cpa-warm" | "cpa-white" | "cpa-dark";

function parentDocument(): Document | null {
  try {
    if (window.parent === window) return null;
    const doc = window.parent.document; // throws when cross-origin
    return doc?.documentElement ? doc : null;
  } catch {
    return null;
  }
}

export function cpaSkin(dataTheme: string | null): Skin {
  if (dataTheme === "dark") return "cpa-dark";
  if (dataTheme === "white") return "cpa-white";
  return "cpa-warm";
}

export function startTheme(root: HTMLElement = document.documentElement): void {
  const set = (skin: Skin) => {
    if (root.dataset.skin !== skin) root.dataset.skin = skin;
  };
  const parent = parentDocument();
  if (parent) {
    // The WebUI floats its own toolbar over the frame's top-right corner.
    root.dataset.host = "cpa";
    const html = parent.documentElement;
    const apply = () => set(cpaSkin(html.getAttribute("data-theme")));
    apply();
    new MutationObserver(apply).observe(html, { attributes: true, attributeFilter: ["data-theme"] });
    return;
  }
  const mq = window.matchMedia("(prefers-color-scheme: dark)");
  const apply = () => set(mq.matches ? "easycli-dark" : "easycli-light");
  apply();
  mq.addEventListener("change", apply);
}
