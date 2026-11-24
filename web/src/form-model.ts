// Account form rules, kept free of rendering so they can be tested. The
// server applies the same rules; checking here only lets errors appear next
// to the field before a round trip.
import type { MessageKey } from "./i18n";
import type { AccountInput, AccountView, CredentialKind, ProxyMode, Source } from "./types";

/** The data-source choices, in display order. Each source brings its own
 *  fields in the form and its own branch of the connection test. */
export const SOURCE_OPTIONS: readonly { value: Source; label: MessageKey; hint: MessageKey }[] = [
  { value: "newapi", label: "source.newapi", hint: "source.newapi.hint" },
  { value: "custom", label: "source.custom", hint: "source.custom.hint" },
];

export const PROXY_OPTIONS: readonly ProxyMode[] = ["follow", "direct"];

export const PAT_PREFIX = "nap_";

export interface FormState {
  name: string;
  source: Source;
  baseUrl: string;
  /** blank on edit = keep the stored credential */
  credential: string;
  userId: string;
  /** as typed; blank = no threshold */
  threshold: string;
  proxy: ProxyMode;
  /** custom query: the script and its placeholder values */
  script: string;
  phBaseUrl: string;
  /** blank on edit = keep the stored value */
  apiKey: string;
  /** blank on edit = keep the stored value */
  accessToken: string;
  phUserId: string;
}

export type Field =
  | "name"
  | "source"
  | "base_url"
  | "credential"
  | "user_id"
  | "low_balance_threshold"
  | "proxy"
  | "custom_script"
  | "custom_placeholders.baseUrl"
  | "custom_placeholders.apiKey"
  | "custom_placeholders.accessToken"
  | "custom_placeholders.userId";

/** Field → reason (`required`, `invalid`, `scheme`, `not_numeric`, `negative`, `unsupported`). */
export type FieldErrors = Partial<Record<Field, string>>;

export function emptyForm(): FormState {
  return {
    name: "",
    source: "newapi",
    baseUrl: "",
    credential: "",
    userId: "",
    threshold: "",
    proxy: "follow",
    script: "",
    phBaseUrl: "",
    apiKey: "",
    accessToken: "",
    phUserId: "",
  };
}

export function formOf(a: AccountView): FormState {
  return {
    name: a.name,
    source: a.source,
    baseUrl: a.source === "custom" ? "" : a.base_url,
    credential: "",
    userId: a.user_id,
    threshold: a.low_balance_threshold == null ? "" : String(a.low_balance_threshold),
    proxy: a.proxy,
    script: a.custom_script,
    phBaseUrl: a.custom_placeholders.baseUrl,
    apiKey: "",
    accessToken: "",
    phUserId: a.custom_placeholders.userId,
  };
}

export function kindOf(credential: string): CredentialKind | null {
  const c = credential.trim();
  if (!c) return null;
  return c.startsWith(PAT_PREFIX) ? "pat" : "legacy";
}

/** The credential the account will use: the typed one, else the stored one. */
export function effectiveKind(f: FormState, stored: AccountView | null): CredentialKind | null {
  return kindOf(f.credential) ?? (stored?.credential_kind || null);
}

/** The user ID belongs to legacy access tokens only. */
export function needsUserId(f: FormState, stored: AccountView | null): boolean {
  return effectiveKind(f, stored) === "legacy";
}

/** Why the server would refuse this site address, or null when it accepts it. */
export function baseUrlError(raw: string): string | null {
  const v = raw.trim();
  if (!v) return "required";
  const scheme = /^([a-z][a-z0-9+.-]*):\/\//i.exec(v)?.[1]?.toLowerCase();
  if (scheme !== "http" && scheme !== "https") return "scheme";
  let u: URL;
  try {
    u = new URL(v);
  } catch {
    return "invalid";
  }
  if (!u.host || u.username || u.password || v.includes("?") || v.includes("#")) return "invalid";
  return null;
}

/** Trailing slashes are not part of a site address. */
export function trimBaseUrl(raw: string): string {
  return raw.trim().replace(/\/+$/, "");
}

function thresholdValue(raw: string): number | null {
  const v = raw.trim();
  return v === "" ? null : Number(v);
}

/** Checks the form; `forTest` skips what a connection test does not need. */
export function validate(f: FormState, stored: AccountView | null, forTest = false): FieldErrors {
  const e: FieldErrors = {};
  if (!forTest && !f.name.trim()) e.name = "required";
  if (f.source === "custom") {
    if (!f.script.trim()) e.custom_script = "required";
    const base = f.phBaseUrl.trim() ? baseUrlError(f.phBaseUrl) : null;
    if (base) e["custom_placeholders.baseUrl"] = base;
  } else {
    const url = baseUrlError(f.baseUrl);
    if (url) e.base_url = url;
    if (!f.credential.trim() && !stored?.credential_masked) e.credential = "required";
    if (needsUserId(f, stored)) {
      const id = f.userId.trim();
      if (!id) e.user_id = "required";
      else if (!/^[0-9]+$/.test(id)) e.user_id = "not_numeric";
    }
  }
  if (!forTest) {
    const t = thresholdValue(f.threshold);
    if (t !== null && !Number.isFinite(t)) e.low_balance_threshold = "invalid";
    else if (t !== null && t < 0) e.low_balance_threshold = "negative";
  }
  return e;
}

export function toInput(f: FormState, stored: AccountView | null): AccountInput {
  const t = thresholdValue(f.threshold);
  const custom = f.source === "custom";
  return {
    name: f.name.trim(),
    source: f.source,
    base_url: custom ? "" : trimBaseUrl(f.baseUrl),
    credential: custom ? "" : f.credential.trim(),
    user_id: !custom && needsUserId(f, stored) ? f.userId.trim() : "",
    low_balance_threshold: t !== null && Number.isFinite(t) ? t : null,
    proxy: f.proxy,
    custom_script: custom ? f.script : "",
    custom_placeholders: custom
      ? { baseUrl: trimBaseUrl(f.phBaseUrl), apiKey: f.apiKey.trim(), accessToken: f.accessToken.trim(), userId: f.phUserId.trim() }
      : { baseUrl: "", apiKey: "", accessToken: "", userId: "" },
  };
}

/** Fields whose change makes an earlier connection test stale. */
export const TESTED_FIELDS: readonly (keyof FormState)[] = [
  "source",
  "baseUrl",
  "credential",
  "userId",
  "proxy",
  "script",
  "phBaseUrl",
  "apiKey",
  "accessToken",
  "phUserId",
];

/** Order in which errors take focus: the form's visual order (one source's fields show at a time). */
export const FIELD_ORDER: readonly Field[] = [
  "name",
  "source",
  "base_url",
  "credential",
  "user_id",
  "custom_script",
  "custom_placeholders.baseUrl",
  "custom_placeholders.apiKey",
  "custom_placeholders.accessToken",
  "custom_placeholders.userId",
  "low_balance_threshold",
  "proxy",
];
