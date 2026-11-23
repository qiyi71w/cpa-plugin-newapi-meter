// Period rules of the detail controls (bun test tests/web).
import { describe, expect, test } from "bun:test";
import { addDays, checkCustom, seriesOf, shapeOf, valuesOf } from "../../web/src/period-model";

const TODAY = "2026-11-23";

describe("custom period input", () => {
  test("an end before the start is swapped, not rejected", () => {
    expect(checkCustom("2026-11-20", "2026-11-10", TODAY)).toEqual({ ok: true, start: "2026-11-10", end: "2026-11-20", swapped: true });
    expect(checkCustom("2026-11-10", "2026-11-20", TODAY)).toEqual({ ok: true, start: "2026-11-10", end: "2026-11-20", swapped: false });
  });

  test("365 days is the limit, counting both ends", () => {
    expect(checkCustom(addDays(TODAY, -364), TODAY, TODAY)).toMatchObject({ ok: true });
    expect(checkCustom(addDays(TODAY, -365), TODAY, TODAY)).toEqual({ ok: false, reason: "too_long", days: 366 });
    // Swapped and too long: the length is still reported.
    expect(checkCustom("2026-11-01", "2025-01-01", TODAY)).toEqual({ ok: false, reason: "too_long", days: 670 });
  });

  test("dates after today are not queried", () => {
    expect(checkCustom("2026-11-20", "2026-11-24", TODAY)).toEqual({ ok: false, reason: "future" });
    expect(checkCustom("2026-11-25", "2026-11-20", TODAY)).toEqual({ ok: false, reason: "future" });
    expect(checkCustom("", "2026-11-20", TODAY)).toEqual({ ok: false, reason: "incomplete" });
  });
});

describe("period shape", () => {
  test("presets and the 2-day granularity switch", () => {
    expect(shapeOf({ preset: "today" }, TODAY)).toEqual({ start: TODAY, end: TODAY, days: 1, granularity: "hour", includesNow: true });
    expect(shapeOf({ preset: "yesterday" }, TODAY)).toMatchObject({ start: "2026-11-22", granularity: "hour", includesNow: false });
    expect(shapeOf({ preset: "7d" }, TODAY)).toMatchObject({ start: "2026-11-17", days: 7, granularity: "day" });
    expect(shapeOf({ preset: "30d" }, TODAY)).toMatchObject({ start: "2026-10-25", days: 30 });
    expect(shapeOf({ preset: "custom", start: "2026-11-21", end: "2026-11-22" }, TODAY)).toMatchObject({ days: 2, granularity: "hour", includesNow: false });
    expect(shapeOf({ preset: "custom", start: "2026-11-20", end: "2026-11-22" }, TODAY)).toMatchObject({ days: 3, granularity: "day" });
  });

  test("calendar arithmetic ignores the browser's zone", () => {
    expect(addDays("2026-03-08", 1)).toBe("2026-03-09");
    expect(addDays("2026-03-01", -1)).toBe("2026-02-28");
  });
});

describe("chart series", () => {
  const model = (i: number, share: number) => ({ model: `m${i}`, spend: share * 10, share, requests: i, tokens: i * 10 });

  test("seven models keep their own series", () => {
    const s = seriesOf(Array.from({ length: 7 }, (_, i) => model(i, 1 / 7)));
    expect(s).toHaveLength(7);
    expect(s[6]).toMatchObject({ model: "m6", color: "var(--nm-s-other)" });
  });

  test("beyond seven, the smallest models merge", () => {
    const s = seriesOf(Array.from({ length: 9 }, (_, i) => model(i, 0.1)));
    expect(s).toHaveLength(7);
    expect(s[6]).toMatchObject({ model: null, count: 3, members: [6, 7, 8] });
    expect(s[6].share).toBeCloseTo(0.3);
    const bucket = { start: "2026-11-23T10:00:00+08:00", spend: [1, 2, 3, 4, 5, 6, 7, 8, 9], requests: [], tokens: [] };
    expect(valuesOf(bucket, "spend", s)).toEqual([1, 2, 3, 4, 5, 6, 24]);
  });
});
