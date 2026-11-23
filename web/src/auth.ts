// Reads the Management Key the host already saved for this origin.
// CPA WebUI stores localStorage["cli-proxy-auth"] as `enc::v2::` (or the
// older `enc::v1::`) reversible obfuscation of a zustand JSON document;
// EasyCLI injects the same document as plain JSON. A key typed into the page
// is kept in memory only (see app.tsx) and never written back.

const STORAGE_KEY = "cli-proxy-auth";
const SALT = "cli-proxy-api-webui::secure-storage";
const V2 = "enc::v2::";
const V1 = "enc::v1::";

function deobfuscate(body: string, secret: string): string {
  const bin = atob(body);
  const key = new TextEncoder().encode(secret);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i) ^ key[i % key.length];
  return new TextDecoder().decode(out);
}

function sameOrigin(apiBase: string, origin: string): boolean {
  try {
    return new URL(apiBase, origin).origin === origin;
  } catch {
    return false;
  }
}

/** Decodes a stored cli-proxy-auth value into the Management Key, or null.
 *  `origin` is this page's origin; `host` and `userAgent` key the obfuscation. */
export function decodeStoredAuth(raw: string, origin: string, host: string, userAgent: string): string | null {
  let text = raw;
  try {
    if (raw.startsWith(V2)) text = deobfuscate(raw.slice(V2.length), `${SALT}|${host}`);
    else if (raw.startsWith(V1)) text = deobfuscate(raw.slice(V1.length), `${SALT}|${host}|${userAgent}`);
    const doc: unknown = JSON.parse(text);
    if (!doc || typeof doc !== "object") return null;
    // zustand persists { state: {...}, version }; accept a bare state object too.
    const state: unknown = "state" in doc ? doc.state : doc;
    if (!state || typeof state !== "object" || !("managementKey" in state)) return null;
    const key = state.managementKey;
    if (typeof key !== "string" || key.trim() === "") return null;
    // A key saved for another server must not be sent to this one.
    const apiBase = "apiBase" in state ? state.apiBase : undefined;
    if (typeof apiBase === "string" && apiBase.trim() !== "" && !sameOrigin(apiBase.trim(), origin)) return null;
    return key.trim();
  } catch {
    return null;
  }
}

/** The saved Management Key for this origin, or null. */
export function readSavedKey(): string | null {
  let raw: string | null = null;
  try {
    raw = localStorage.getItem(STORAGE_KEY);
  } catch {
    return null;
  }
  return raw ? decodeStoredAuth(raw, location.origin, location.host, navigator.userAgent) : null;
}
