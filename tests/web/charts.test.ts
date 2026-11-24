// Chart hover rules (bun test tests/web).
import { describe, expect, test } from "bun:test";
import { hoverAt } from "../../web/src/charts";

describe("chart hover", () => {
  test("an index inside the points is hovered", () => {
    expect(hoverAt(0, 7)).toBe(0);
    expect(hoverAt(6, 7)).toBe(6);
    expect(hoverAt(null, 7)).toBeNull();
  });

  test("an index left over from longer data hovers nothing", () => {
    // 30 daily points were hovered at day 29, then a 7-day period arrived.
    expect(hoverAt(29, 7)).toBeNull();
    expect(hoverAt(7, 7)).toBeNull();
    expect(hoverAt(0, 0)).toBeNull();
    expect(hoverAt(-1, 7)).toBeNull();
  });
});
