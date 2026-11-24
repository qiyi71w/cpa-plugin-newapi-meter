// Shapes of the plugin management routes (see README, "Management routes").

export type AccountState = "ok" | "low" | "partial" | "needs_action";

export type FailureCode =
  | "site_unreachable"
  | "legacy_token_retired"
  | "pat_scope_denied"
  | "credential_invalid"
  | "site_blocked"
  | "site_error"
  | "bad_response"
  | "script_error"
  | "query_timeout"
  | "reported_invalid";

export interface Failure {
  code: FailureCode;
  http_status?: number;
  diagnostic: string;
}

export interface Probe {
  endpoint: string;
  credentialed: boolean;
  failure?: Failure;
}

export interface Currency {
  /** USD | CNY | CUSTOM | TOKENS, or UNIT for a custom-query unit shown verbatim (symbol = the unit) */
  code: string;
  symbol: string;
}

export interface OverviewAccount {
  id: string;
  name: string;
  source: "newapi" | "custom";
  base_url: string;
  host: string;
  credential_kind: "pat" | "legacy" | "";
  state: AccountState;
  site_version: string;
  currency: Currency | null;
  data_export: boolean | null;
  balance: number | null;
  total_used: number | null;
  today_spend: number | null;
  rpm: number | null;
  tpm: number | null;
  low_balance_threshold: number | null;
  balance_failure: Failure | null;
  today_failure: Failure | null;
  primary_failure: Failure | null;
  failures: Probe[];
  fetched_at: string;
  last_success_at: string | null;
  cached: boolean;
  unsupported_source?: boolean;
  /** custom-query accounts only */
  custom?: CustomView;
}

export interface Overview {
  accounts: OverviewAccount[];
  generated_at: string;
  today_start: string;
  timezone: string;
}

export interface ApiErrorBody {
  code: string;
  field?: string;
  reason?: string;
  path?: string;
}

export type Source = "newapi" | "custom";
export type ProxyMode = "follow" | "direct";
export type CredentialKind = "pat" | "legacy";

/** An account as listed by GET /accounts: secrets only ever masked. */
export interface AccountView {
  id: string;
  order: number;
  name: string;
  source: Source;
  base_url: string;
  credential_masked: string;
  credential_kind: CredentialKind | "";
  user_id: string;
  low_balance_threshold: number | null;
  proxy: ProxyMode;
  custom_script: string;
  custom_placeholders: { baseUrl: string; apiKeyMasked: string; accessTokenMasked: string; userId: string };
}

export interface AccountList {
  accounts: AccountView[];
  data_file: string;
}

/** Body of POST/PUT /accounts and POST /accounts/test. A blank credential
 *  keeps (or, for a test, uses) the stored one of `id`. */
export interface AccountInput {
  name: string;
  source: Source;
  base_url: string;
  credential: string;
  user_id: string;
  low_balance_threshold: number | null;
  proxy: ProxyMode;
  custom_script: string;
  /** blank apiKey / accessToken keep (or, for a test, use) the stored ones of `id` */
  custom_placeholders: { baseUrl: string; apiKey: string; accessToken: string; userId: string };
}

/** Answer of POST /accounts/test. */
export interface ConnectionTest {
  source: Source;
  ok: boolean;
  /** ok | partial | needs_action (never low: no threshold is applied) */
  state: AccountState;
  credential_kind: CredentialKind | "";
  site_version: string;
  currency: Currency | null;
  data_export: boolean | null;
  failure: Failure | null;
  failures: Probe[];
  /** custom-query tests only: the extraction result or the script error */
  custom?: CustomTest;
}

/** One item the custom-query extractor returned. Strings come from the script. */
export interface CustomPlan {
  plan_name: string;
  extra: string;
  remaining: number | null;
  used: number | null;
  total: number | null;
  today_spend: number | null;
  unit: string;
  currency: Currency;
  is_valid: boolean | null;
  invalid_message?: string;
}

export interface CustomView {
  /** sum: every plan has one unit (合计); first: units differ (第一项); absent for a single item */
  balance_basis?: "sum" | "first";
  total_quota: number | null;
  plans: CustomPlan[];
  invalid_message?: string;
  /** engine or script error, substituted secrets masked; shown verbatim */
  script_error?: string;
  /** a failure the plugin explains itself; the page words it (custom.reason.<code>) */
  script_reason?: ScriptReason;
}

export interface ScriptReason {
  code: string;
  /** the request/result field it is about, or for "uncaught" the script position */
  field?: string;
}

export interface CustomTest extends CustomView {
  balance: number | null;
  total_used: number | null;
  today_spend: number | null;
}

export type PeriodPreset = "today" | "yesterday" | "7d" | "30d" | "custom";

/** The selected period; custom carries its applied dates (YYYY-MM-DD, CPA's zone). */
export interface PeriodChoice {
  preset: PeriodPreset;
  start?: string;
  end?: string;
}

export interface ModelUsage {
  model: string;
  spend: number;
  /** of the period's spend, 0..1 */
  share: number;
  requests: number;
  tokens: number;
}

/** One hour or day; the arrays follow Usage.models. */
export interface UsageBucket {
  /** RFC 3339 in CPA's zone, e.g. 2026-11-23T10:00:00+08:00 */
  start: string;
  spend: number[];
  requests: number[];
  tokens: number[];
}

export interface Usage {
  spend: number;
  requests: number;
  tokens: number;
  avg_rpm: number | null;
  avg_tpm: number | null;
  models: ModelUsage[];
  buckets: UsageBucket[];
}

export interface PeriodInfo {
  preset: PeriodPreset;
  start_date: string;
  end_date: string;
  days: number;
  start: string;
  end: string;
  granularity: "hour" | "day";
  includes_now: boolean;
  minutes: number;
}

export interface AccountDetail {
  id: string;
  period: PeriodInfo;
  timezone: string;
  currency: Currency | null;
  data_export: boolean | null;
  spend: number | null;
  spend_failure: Failure | null;
  usage: Usage | null;
  usage_failure: Failure | null;
  usage_disabled: boolean;
  fetched_at: string;
  cached: boolean;
}
