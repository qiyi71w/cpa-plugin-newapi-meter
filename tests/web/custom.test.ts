// Custom-query presentation: plugin-authored script reasons are worded by
// the page. Tests run with the default language (zh); the en table has the
// same keys by type (Record<MessageKey, string>).
import { describe, expect, test } from "bun:test";
import { reasonText } from "../../web/src/custom";
import { lang } from "../../web/src/i18n";
import { totals } from "../../web/src/model";
import type { OverviewAccount } from "../../web/src/types";

// Every customquery.Reason* code the plugin sends.
const CODES = [
  "not_config",
  "request_missing",
  "extractor_missing",
  "headers_not_object",
  "body_not_string",
  "request_unreadable",
  "url_invalid",
  "url_scheme",
  "method_invalid",
  "header_name_invalid",
  "header_value_invalid",
  "result_missing",
  "result_unreadable",
  "result_empty",
  "item_not_object",
  "field_not_number",
  "field_not_string",
  "field_not_boolean",
  "script_failed",
  "uncaught",
];

describe("script reasons", () => {
  test("every code has a zh sentence, with the field filled in", () => {
    expect(lang).toBe("zh");
    for (const code of CODES) {
      const text = reasonText({ code, field: "result[1].remaining" });
      expect(text).not.toBe(code);
      expect(text).toMatch(/[\u4e00-\u9fff]/);
      expect(text).not.toContain("{field}");
    }
  });

  test("R12 cases read in zh", () => {
    expect(reasonText({ code: "url_scheme" })).toBe("request.url 只能使用 http 或 https。");
    expect(reasonText({ code: "header_name_invalid" })).toBe("request.headers 里有不合法的请求头名称。");
    expect(reasonText({ code: "field_not_number", field: "result.remaining" })).toBe("result.remaining 必须是数字或 null。");
  });

  test("an uncaught value names its position only when known", () => {
    expect(reasonText({ code: "uncaught", field: "script:2:3" })).toContain("script:2:3");
    expect(reasonText({ code: "uncaught" })).toBe("脚本抛出了一个无法转成文字的值。");
  });
});

const usd = { code: "USD", symbol: "$" };
const cny = { code: "CNY", symbol: "¥" };
const acc = (o: Partial<OverviewAccount>) =>
  ({ id: "a", name: "A", source: "newapi", state: "ok", currency: usd, balance: 1, today_spend: 1, today_spend_currency: usd, ...o }) as OverviewAccount;

describe("today's spend unit", () => {
  test("R3: a CNY today spend is not added to a USD total", () => {
    const mixed = acc({ id: "mixed", source: "custom", balance: 10, today_spend: 7, today_spend_currency: cny });
    const tot = totals([mixed, acc({ id: "b", balance: 3, today_spend: 1 })]);
    expect(tot?.balance).toBe(13);
    expect(tot?.today).toBe(1);
  });
});
