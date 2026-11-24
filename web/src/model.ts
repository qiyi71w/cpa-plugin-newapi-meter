import { sameCurrency } from "./format";
import type { Currency, OverviewAccount } from "./types";

/** A full gauge is this many thresholds of balance; the tick marks 1×. */
export const GAUGE_FULL = 4;

/** Default tab: the first low-balance account, else the first account. */
export function defaultSelection(accounts: OverviewAccount[]): string | null {
  return (accounts.find((a) => a.state === "low") ?? accounts[0])?.id ?? null;
}

/** Only normal and low-balance accounts have a trustworthy balance to add up. */
export function countsTowardTotal(a: OverviewAccount): boolean {
  return a.state === "ok" || a.state === "low";
}

export interface Totals {
  currency: Currency;
  balance: number;
  today: number;
  excluded: number;
}

/** The "all accounts" cell appears when there are several accounts and every
 *  account that counts shares one display currency. */
export function totals(accounts: OverviewAccount[]): Totals | null {
  const counted = accounts.filter(countsTowardTotal);
  if (accounts.length < 2 || counted.length === 0) return null;
  const currency = counted[0].currency;
  if (!currency || !counted.every((a) => sameCurrency(a.currency, currency))) return null;
  return {
    currency,
    balance: counted.reduce((s, a) => s + (a.balance ?? 0), 0),
    // Today's spend can be in another unit than the balance (custom queries).
    today: counted.reduce((s, a) => s + (a.today_spend != null && sameCurrency(a.today_spend_currency, currency) ? a.today_spend : 0), 0),
    excluded: accounts.length - counted.length,
  };
}

export type GaugeForm = "fill" | "low" | "none" | "unknown" | "error";

export interface Gauge {
  form: GaugeForm;
  /** 0..1 of the bar */
  ratio: number;
  /** balance ÷ threshold, for the tooltip */
  multiple: number;
}

/** The 3px headroom gauge under each tab. */
export function gauge(a: OverviewAccount): Gauge {
  if (a.state === "needs_action") return { form: "error", ratio: 0, multiple: 0 };
  if (a.state === "partial" || a.balance == null) return { form: "unknown", ratio: 0, multiple: 0 };
  const th = a.low_balance_threshold;
  if (th == null) return { form: "none", ratio: 0, multiple: 0 };
  const multiple = th > 0 ? a.balance / th : a.balance > 0 ? Infinity : 0;
  return {
    form: a.state === "low" ? "low" : "fill",
    ratio: Math.max(0, Math.min(1, multiple / GAUGE_FULL)),
    multiple,
  };
}
