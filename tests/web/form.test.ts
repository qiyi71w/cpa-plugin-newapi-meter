// Account form rules (bun test tests/web).
import { describe, expect, test } from "bun:test";
import { baseUrlError, emptyForm, formOf, needsUserId, toInput, validate, type FormState } from "../../web/src/form-model";
import { money } from "../../web/src/format";
import { totals } from "../../web/src/model";
import type { AccountView, OverviewAccount } from "../../web/src/types";

const PAT = "nap_synthetic_form_token_0001";
const LEGACY = "synthetic-legacy-form-token-0002";

const form = (over: Partial<FormState>): FormState => ({ ...emptyForm(), name: "Main", baseUrl: "https://newapi.example.com", ...over });

const stored: AccountView = {
  id: "acc_0000000000000001",
  order: 0,
  name: "Stored",
  source: "newapi",
  base_url: "https://newapi.example.com",
  credential_masked: "synt••••0002",
  credential_kind: "legacy",
  user_id: "42",
  low_balance_threshold: 5,
  proxy: "follow",
  custom_script: "",
  custom_placeholders: { baseUrl: "", apiKeyMasked: "", accessTokenMasked: "", userId: "" },
};

describe("credential kind and user ID", () => {
  test("a PAT needs no user ID; any other credential does", () => {
    expect(needsUserId(form({ credential: PAT }), null)).toBe(false);
    expect(needsUserId(form({ credential: LEGACY }), null)).toBe(true);
    expect(needsUserId(form({ credential: "" }), null)).toBe(false);
  });

  test("on edit a blank credential follows the stored kind", () => {
    expect(needsUserId(form({ credential: "" }), stored)).toBe(true);
    expect(needsUserId(form({ credential: PAT }), stored)).toBe(false);
  });

  test("a legacy credential without a numeric user ID is rejected", () => {
    expect(validate(form({ credential: LEGACY }), null).user_id).toBe("required");
    expect(validate(form({ credential: LEGACY, userId: "4x" }), null).user_id).toBe("not_numeric");
    expect(validate(form({ credential: LEGACY, userId: "42" }), null)).toEqual({});
  });

  test("the user ID is not sent for a PAT", () => {
    expect(toInput(form({ credential: PAT, userId: "42" }), null).user_id).toBe("");
  });
});

describe("form validation", () => {
  test("site address must be http(s) without query, fragment or user", () => {
    expect(baseUrlError("")).toBe("required");
    expect(baseUrlError("newapi.example.com")).toBe("scheme");
    expect(baseUrlError("ftp://newapi.example.com")).toBe("scheme");
    expect(baseUrlError("https://newapi.example.com/?a=1")).toBe("invalid");
    expect(baseUrlError("https://user:pw@newapi.example.com")).toBe("invalid");
    expect(baseUrlError("https://newapi.example.com/")).toBeNull();
    expect(toInput(form({ credential: PAT, baseUrl: " https://newapi.example.com/// " }), null).base_url).toBe("https://newapi.example.com");
  });

  test("new accounts need a credential; edits may keep the stored one", () => {
    expect(validate(form({ credential: "" }), null).credential).toBe("required");
    expect(validate(form({ credential: "", userId: "42" }), stored)).toEqual({});
  });

  test("threshold is optional, numeric and not negative", () => {
    expect(validate(form({ credential: PAT, threshold: "" }), null)).toEqual({});
    expect(validate(form({ credential: PAT, threshold: "abc" }), null).low_balance_threshold).toBe("invalid");
    expect(validate(form({ credential: PAT, threshold: "-1" }), null).low_balance_threshold).toBe("negative");
    expect(toInput(form({ credential: PAT, threshold: "2.5" }), null).low_balance_threshold).toBe(2.5);
    expect(toInput(form({ credential: PAT, threshold: " " }), null).low_balance_threshold).toBeNull();
  });

  test("a connection test needs neither name nor threshold", () => {
    const e = validate(form({ name: "", credential: PAT, threshold: "abc" }), null, true);
    expect(e).toEqual({});
    expect(validate(form({ name: "" , credential: PAT }), null).name).toBe("required");
  });
});

describe("custom query form", () => {
  const custom = (over: Partial<FormState>): FormState => ({ ...emptyForm(), name: "Script", source: "custom", script: "({})", ...over });
  const storedCustom: AccountView = {
    ...stored,
    source: "custom",
    base_url: "https://api.example.com",
    credential_masked: "",
    credential_kind: "",
    user_id: "",
    custom_script: "({ request: {}, extractor: function () {} })",
    custom_placeholders: { baseUrl: "https://api.example.com", apiKeyMasked: "sk-s••••0003", accessTokenMasked: "", userId: "u-1" },
  };

  test("needs a script; baseUrl is optional but must be http(s); no credential or user ID", () => {
    expect(validate(custom({}), null)).toEqual({});
    expect(validate(custom({ script: "  " }), null).custom_script).toBe("required");
    expect(validate(custom({ phBaseUrl: "ftp://api.example.com" }), null)["custom_placeholders.baseUrl"]).toBe("scheme");
    expect(validate(custom({ phBaseUrl: "https://api.example.com/?a=1" }), null)["custom_placeholders.baseUrl"]).toBe("invalid");
    expect(validate(custom({ credential: "legacy-token" }), null)).toEqual({});
  });

  test("sends the script and placeholder values only; blank secrets keep the stored ones", () => {
    const input = toInput(custom({ phBaseUrl: " https://api.example.com/ ", apiKey: " sk-new ", phUserId: " u-2 ", baseUrl: "https://x.example.com", credential: "nap_x" }), storedCustom);
    expect(input.custom_script).toBe("({})");
    expect(input.custom_placeholders).toEqual({ baseUrl: "https://api.example.com", apiKey: "sk-new", accessToken: "", userId: "u-2" });
    expect(input.base_url).toBe("");
    expect(input.credential).toBe("");
    const edit = formOf(storedCustom);
    expect(edit.apiKey).toBe("");
    expect(edit.script).toBe(storedCustom.custom_script);
    expect(edit.phUserId).toBe("u-1");
  });
});

describe("custom units", () => {
  test("units other than USD and CNY are shown verbatim", () => {
    expect(money(12.5, { code: "UNIT", symbol: "credits" })).toBe("12.50 credits");
    expect(money(3, { code: "UNIT", symbol: "" })).toBe("3.00");
    expect(money(3, { code: "USD", symbol: "$" })).toBe("$3.00");
  });

  test("the total cell compares units: a custom USD account adds up with a built-in one", () => {
    const acc = (id: string, currency: OverviewAccount["currency"], balance: number): OverviewAccount =>
      ({ id, state: "ok", currency, balance, today_spend: 1 }) as OverviewAccount;
    const usd = { code: "USD", symbol: "$" };
    expect(totals([acc("a", usd, 2), acc("b", usd, 3)])?.balance).toBe(5);
    expect(totals([acc("a", usd, 2), acc("b", { code: "UNIT", symbol: "credits" }, 3)])).toBeNull();
    expect(totals([acc("a", { code: "UNIT", symbol: "credits" }, 2), acc("b", { code: "UNIT", symbol: "credits" }, 3)])?.balance).toBe(5);
  });
});
