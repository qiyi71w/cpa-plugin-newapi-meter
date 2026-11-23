// Shapes of the plugin management routes (see README, "Management routes").

export type AccountState = "ok" | "low" | "partial" | "needs_action";

export type FailureCode =
  | "site_unreachable"
  | "legacy_token_retired"
  | "pat_scope_denied"
  | "credential_invalid"
  | "site_blocked"
  | "site_error"
  | "bad_response";

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
  /** USD | CNY | CUSTOM | TOKENS */
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
