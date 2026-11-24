// Period rules shared by the controls and charts. Dates are YYYY-MM-DD
// strings in CPA's time zone; they are only ever compared and shifted as
// calendar dates, never through the browser's zone.
import type { ModelUsage, PeriodChoice, PeriodPreset, UsageBucket } from "./types";

export const PRESETS: PeriodPreset[] = ["today", "yesterday", "7d", "30d", "custom"];
/** Longest period, counting both end days (the backend enforces the same). */
export const MAX_DAYS = 365;
/** Periods up to this many days are bucketed by hour. */
const HOURLY_MAX_DAYS = 2;

function utc(date: string): number {
  const [y, m, d] = date.split("-").map(Number);
  return Date.UTC(y, m - 1, d);
}

export function addDays(date: string, n: number): string {
  return new Date(utc(date) + n * 86_400_000).toISOString().slice(0, 10);
}

/** Calendar days from a to b (negative when b is earlier). */
export function daysBetween(a: string, b: string): number {
  return Math.round((utc(b) - utc(a)) / 86_400_000);
}

export interface Range {
  start: string;
  end: string;
}

/** The dates a choice covers on the given CPA-local today. */
export function rangeOf(c: PeriodChoice, today: string): Range {
  switch (c.preset) {
    case "yesterday":
      return { start: addDays(today, -1), end: addDays(today, -1) };
    case "7d":
      return { start: addDays(today, -6), end: today };
    case "30d":
      return { start: addDays(today, -29), end: today };
    case "custom":
      return { start: c.start ?? today, end: c.end ?? today };
    default:
      return { start: today, end: today };
  }
}

export interface PeriodShape extends Range {
  days: number;
  granularity: "hour" | "day";
  /** the period runs up to now, so the latest usage is still arriving */
  includesNow: boolean;
}

export function shapeOf(c: PeriodChoice, today: string): PeriodShape {
  const r = rangeOf(c, today);
  const days = daysBetween(r.start, r.end) + 1;
  return { ...r, days, granularity: days <= HOURLY_MAX_DAYS ? "hour" : "day", includesNow: r.end === today };
}

export type CustomCheck =
  | { ok: true; start: string; end: string; swapped: boolean }
  | { ok: false; reason: "incomplete" | "future" | "too_long"; days?: number };

/** Validates custom dates from the inputs: an end before the start is
 *  swapped rather than rejected; future dates and periods over MAX_DAYS are not
 *  queried. */
export function checkCustom(start: string, end: string, today: string): CustomCheck {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(start) || !/^\d{4}-\d{2}-\d{2}$/.test(end)) return { ok: false, reason: "incomplete" };
  const swapped = start > end;
  if (swapped) [start, end] = [end, start];
  if (end > today) return { ok: false, reason: "future" };
  const days = daysBetween(start, end) + 1;
  if (days > MAX_DAYS) return { ok: false, reason: "too_long", days };
  return { ok: true, start, end, swapped };
}

export interface CustomCommit {
  /** what the inputs show after the commit */
  draft: Range;
  /** the range to query, or null when nothing new is applied */
  apply: Range | null;
  hint: "swapped" | "too_long" | "future" | null;
  days?: number;
}

/** Commits the dates in the inputs once an edit is complete (focus left the
 *  date pair, Enter, or a picker selection); keystrokes before that are only
 *  a draft. A reversed pair is swapped; a too-long or future range is kept on
 *  screen with its hint but not queried; an incomplete pair falls back to the
 *  applied range; an unchanged range is not queried again. */
export function commitCustom(draft: Range, applied: Range, today: string): CustomCommit {
  const c = checkCustom(draft.start, draft.end, today);
  if (!c.ok) return c.reason === "incomplete" ? { draft: applied, apply: null, hint: null } : { draft, apply: null, hint: c.reason, days: c.days };
  const next = { start: c.start, end: c.end };
  const same = next.start === applied.start && next.end === applied.end;
  return { draft: next, apply: same ? null : next, hint: c.swapped ? "swapped" : null };
}

export function sameChoice(a: PeriodChoice, b: PeriodChoice): boolean {
  return a.preset === b.preset && (a.preset !== "custom" || (a.start === b.start && a.end === b.end));
}

/** Chart colors: six series tokens, then one shared neutral. */
export const SERIES_COLORS = ["var(--nm-s1)", "var(--nm-s2)", "var(--nm-s3)", "var(--nm-s4)", "var(--nm-s5)", "var(--nm-s6)"];
export const OTHER_COLOR = "var(--nm-s-other)";

/** One drawn series: a model, or several small models merged ("others"). */
export interface Series {
  /** model name; null for the merged series */
  model: string | null;
  /** merged series: how many models it holds */
  count: number;
  color: string;
  /** indexes into Usage.models */
  members: number[];
  spend: number;
  share: number;
}

/** Up to seven series: the six largest models in their own colors, then a
 *  seventh model as is, or every remaining model merged into one. */
export function seriesOf(models: ModelUsage[]): Series[] {
  const own = models.length <= SERIES_COLORS.length + 1 ? models.length : SERIES_COLORS.length;
  const out: Series[] = models.slice(0, own).map((m, i) => ({
    model: m.model,
    count: 1,
    color: SERIES_COLORS[i] ?? OTHER_COLOR,
    members: [i],
    spend: m.spend,
    share: m.share,
  }));
  if (own < models.length) {
    const rest = models.slice(own);
    out.push({
      model: null,
      count: rest.length,
      color: OTHER_COLOR,
      members: rest.map((_, i) => own + i),
      spend: rest.reduce((s, m) => s + m.spend, 0),
      share: rest.reduce((s, m) => s + m.share, 0),
    });
  }
  return out;
}

/** A model's color in the table: its series color, or the merged one. */
export function colorOf(index: number, series: Series[]): string {
  return series.find((s) => s.members.includes(index))?.color ?? OTHER_COLOR;
}

export type Metric = "spend" | "requests" | "tokens";

/** A bucket's values per series. */
export function valuesOf(b: UsageBucket, metric: Metric, series: Series[]): number[] {
  const row = b[metric];
  return series.map((s) => s.members.reduce((sum, i) => sum + (row[i] ?? 0), 0));
}

/** "2026-11-23T10:00:00+08:00" → date and hour as written in CPA's zone. */
export function bucketParts(start: string): { date: string; hour: number } {
  return { date: start.slice(0, 10), hour: Number(start.slice(11, 13)) };
}
