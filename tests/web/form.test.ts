// Account form rules (bun test tests/web).
import { describe, expect, test } from "bun:test";
import { baseUrlError, emptyForm, needsUserId, toInput, validate, type FormState } from "../../web/src/form-model";
import type { AccountView } from "../../web/src/types";

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
