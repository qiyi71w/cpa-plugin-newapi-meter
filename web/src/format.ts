import { locale, t, tDynamic } from "./i18n";
import type { Currency } from "./types";

const compactFmt = new Intl.NumberFormat(locale, { notation: "compact", maximumFractionDigits: 1 });
const rateFmt = new Intl.NumberFormat(locale, { maximumFractionDigits: 1 });
const moneyFmts = new Map<number, Intl.NumberFormat>();

function moneyFmt(maxDigits: number): Intl.NumberFormat {
  let f = moneyFmts.get(maxDigits);
  if (!f) {
    f = new Intl.NumberFormat(locale, { minimumFractionDigits: 2, maximumFractionDigits: maxDigits });
    moneyFmts.set(maxDigits, f);
  }
  return f;
}

export function compact(v: number): string {
  return compactFmt.format(v);
}

/** An amount in the account's display currency; "—" when unknown. A
 *  custom-query unit other than USD/CNY follows the number verbatim. */
export function money(v: number | null | undefined, c: Currency | null | undefined): string {
  if (v == null || !Number.isFinite(v) || !c) return "—";
  if (c.code === "TOKENS") return `${compact(v)} tokens`;
  const abs = Math.abs(v);
  // Small amounts keep enough digits to stay distinguishable from zero.
  const digits = abs >= 1 || abs === 0 ? 2 : abs >= 0.01 ? 4 : 6;
  if (c.code === "UNIT") return `${v < 0 ? "-" : ""}${moneyFmt(digits).format(abs)}${c.symbol ? ` ${c.symbol}` : ""}`;
  return `${v < 0 ? "-" : ""}${c.symbol}${moneyFmt(digits).format(abs)}`;
}

/** The name of a display currency, as shown next to thresholds and test results. */
export function currencyLabel(c: Currency): string {
  if (c.code === "CUSTOM") return t("currency.CUSTOM", { symbol: c.symbol });
  if (c.code === "UNIT") return c.symbol || t("currency.unitless");
  return tDynamic(`currency.${c.code}`, c.code);
}

export function rate(v: number | null | undefined): string {
  return v == null ? "—" : rateFmt.format(v);
}

export function sameCurrency(a: Currency | null, b: Currency | null): boolean {
  return !!a && !!b && a.code === b.code && a.symbol === b.symbol;
}

const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });

/** "刚刚" / "3 分钟前" style age of an ISO timestamp. */
export function ago(iso: string, nowMs: number): string {
  const s = Math.max(0, Math.round((nowMs - Date.parse(iso)) / 1000));
  if (s < 45) return t("time.justNow");
  const m = Math.round(s / 60);
  if (m < 60) return rtf.format(-m, "minute");
  const h = Math.round(m / 60);
  if (h < 24) return rtf.format(-h, "hour");
  return rtf.format(-Math.round(h / 24), "day");
}

const stampFmt = new Intl.DateTimeFormat(locale, { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" });

/** Absolute local date and time for tooltips and "last success" lines. */
export function stamp(iso: string): string {
  return stampFmt.format(new Date(iso));
}

const countFmt = new Intl.NumberFormat(locale, { maximumFractionDigits: 0 });

/** Whole counts (requests) with grouping; compact from a million up. */
export function count(v: number): string {
  return Math.abs(v) >= 1e6 ? compact(v) : countFmt.format(v);
}

/** Full count for tooltips next to compact figures. */
export function fullCount(v: number): string {
  return countFmt.format(v);
}

// CPA-local calendar dates (YYYY-MM-DD) are formatted as UTC dates so the
// browser's own zone never shifts them.
const dayFmt = new Intl.DateTimeFormat(locale, { month: "short", day: "numeric", timeZone: "UTC" });
const dayYearFmt = new Intl.DateTimeFormat(locale, { year: "numeric", month: "short", day: "numeric", timeZone: "UTC" });
const dayShortFmt = new Intl.DateTimeFormat(locale, { month: "numeric", day: "numeric", timeZone: "UTC" });
const weekdayFmt = new Intl.DateTimeFormat(locale, { weekday: "short", timeZone: "UTC" });

function utcDate(date: string): Date {
  const [y, m, d] = date.split("-").map(Number);
  return new Date(Date.UTC(y, m - 1, d));
}

/** "11月23日" / "Nov 23"; with the year when it is not the current one. */
export function dayLabel(date: string, currentYear: string): string {
  return (date.slice(0, 4) === currentYear ? dayFmt : dayYearFmt).format(utcDate(date));
}

/** Axis label: "11/23". */
export function dayTick(date: string): string {
  return dayShortFmt.format(utcDate(date));
}

export function weekday(date: string): string {
  return weekdayFmt.format(utcDate(date));
}
