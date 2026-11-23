import type { AccountInput, AccountList, AccountView, ApiErrorBody, ConnectionTest, Overview } from "./types";

const PAGE_SUFFIX = "/v0/resource/plugins/newapi-meter/index.html";

/** Management routes live on the page's own origin, under any path prefix a
 *  reverse proxy put in front of CPA. */
function routeBase(): string {
  const path = location.pathname;
  const prefix = path.endsWith(PAGE_SUFFIX) ? path.slice(0, -PAGE_SUFFIX.length) : "";
  return `${prefix}/v0/management/plugins/newapi-meter`;
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly body: ApiErrorBody | null,
  ) {
    super(`HTTP ${status}`);
  }
}

/** The request never reached CPA (offline, CPA stopped). */
export class NetworkError extends Error {}

async function request<T>(key: string, path: string, init?: { method: string; body?: unknown }): Promise<T> {
  let res: Response;
  try {
    const headers: Record<string, string> = { Authorization: `Bearer ${key}`, Accept: "application/json" };
    if (init?.body !== undefined) headers["Content-Type"] = "application/json";
    res = await fetch(routeBase() + path, {
      method: init?.method ?? "GET",
      headers,
      body: init?.body === undefined ? undefined : JSON.stringify(init.body),
      cache: "no-store",
      credentials: "omit",
    });
  } catch {
    throw new NetworkError("network");
  }
  const text = await res.text();
  let json: unknown = null;
  try {
    json = text ? JSON.parse(text) : null;
  } catch {
    json = null;
  }
  if (!res.ok) {
    let body: ApiErrorBody | null = null;
    if (json && typeof json === "object" && "error" in json) {
      const err = json.error;
      if (err && typeof err === "object" && "code" in err && typeof err.code === "string") {
        body = {
          code: err.code,
          field: "field" in err && typeof err.field === "string" ? err.field : undefined,
          reason: "reason" in err && typeof err.reason === "string" ? err.reason : undefined,
          path: "path" in err && typeof err.path === "string" ? err.path : undefined,
        };
      }
    }
    throw new ApiError(res.status, body);
  }
  // The plugin owns this route and its response shape (types.ts mirrors it).
  const data = json as T;
  return data;
}

export interface OverviewOptions {
  force?: boolean;
  /** with force: only this account skips the cache */
  id?: string;
}

export function getOverview(key: string, opts: OverviewOptions = {}): Promise<Overview> {
  const q = new URLSearchParams();
  if (opts.force) q.set("force", "1");
  if (opts.id) q.set("id", opts.id);
  const qs = q.toString();
  return request<Overview>(key, `/overview${qs ? `?${qs}` : ""}`);
}

export function listAccounts(key: string): Promise<AccountList> {
  return request<AccountList>(key, "/accounts");
}

export async function createAccount(key: string, input: AccountInput): Promise<AccountView> {
  return (await request<{ account: AccountView }>(key, "/accounts", { method: "POST", body: input })).account;
}

export async function updateAccount(key: string, id: string, input: AccountInput): Promise<AccountView> {
  return (await request<{ account: AccountView }>(key, "/accounts", { method: "PUT", body: { id, ...input } })).account;
}

export async function deleteAccount(key: string, id: string): Promise<void> {
  await request<unknown>(key, `/accounts?${new URLSearchParams({ id })}`, { method: "DELETE" });
}

/** Tries unsaved form values once; with `id`, a blank credential means the stored one. Never saves. */
export function testConnection(key: string, input: AccountInput, id?: string): Promise<ConnectionTest> {
  return request<ConnectionTest>(key, "/accounts/test", { method: "POST", body: id ? { id, ...input } : input });
}
