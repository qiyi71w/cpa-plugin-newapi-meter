// Management Key formats the page must read (bun test tests/web).
import { describe, expect, test } from "bun:test";
import { decodeStoredAuth } from "../../web/src/auth";

const ORIGIN = "http://127.0.0.1:8317";
const HOST = "127.0.0.1:8317";
const UA = "Synthetic/1.0";
const SALT = "cli-proxy-api-webui::secure-storage";
const KEY = "synthetic-management-key";

// Same reversible obfuscation as the CPA WebUI's secure storage.
function obfuscate(prefix: string, value: string, secret: string): string {
  const data = new TextEncoder().encode(value);
  const key = new TextEncoder().encode(secret);
  let bin = "";
  for (let i = 0; i < data.length; i++) bin += String.fromCharCode(data[i] ^ key[i % key.length]);
  return prefix + btoa(bin);
}

const doc = (state: object) => JSON.stringify({ state, version: 0 });

describe("decodeStoredAuth", () => {
  test("plain JSON as injected by EasyCLI", () => {
    expect(decodeStoredAuth(doc({ apiBase: ORIGIN, managementKey: KEY }), ORIGIN, HOST, UA)).toBe(KEY);
    expect(decodeStoredAuth(JSON.stringify({ managementKey: KEY }), ORIGIN, HOST, UA)).toBe(KEY);
  });

  test("enc::v2:: written by the CPA WebUI", () => {
    const raw = obfuscate("enc::v2::", doc({ apiBase: ORIGIN, managementKey: KEY, rememberPassword: true }), `${SALT}|${HOST}`);
    expect(decodeStoredAuth(raw, ORIGIN, HOST, UA)).toBe(KEY);
    // Obfuscated for another host: garbage, not a key.
    expect(decodeStoredAuth(raw, "http://other.example.com", "other.example.com", UA)).toBeNull();
  });

  test("legacy enc::v1:: keyed with the user agent", () => {
    const raw = obfuscate("enc::v1::", doc({ apiBase: ORIGIN, managementKey: KEY }), `${SALT}|${HOST}|${UA}`);
    expect(decodeStoredAuth(raw, ORIGIN, HOST, UA)).toBe(KEY);
  });

  test("a key saved for another origin is not used", () => {
    expect(decodeStoredAuth(doc({ apiBase: "http://cpa.example.com:8317", managementKey: KEY }), ORIGIN, HOST, UA)).toBeNull();
    expect(decodeStoredAuth(doc({ apiBase: `${ORIGIN}/`, managementKey: KEY }), ORIGIN, HOST, UA)).toBe(KEY);
  });

  test("missing or unreadable keys", () => {
    expect(decodeStoredAuth(doc({ apiBase: ORIGIN, rememberPassword: false }), ORIGIN, HOST, UA)).toBeNull();
    expect(decodeStoredAuth(doc({ managementKey: "  " }), ORIGIN, HOST, UA)).toBeNull();
    expect(decodeStoredAuth("not json", ORIGIN, HOST, UA)).toBeNull();
    expect(decodeStoredAuth("enc::v2::%%%", ORIGIN, HOST, UA)).toBeNull();
  });
});
