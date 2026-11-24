// The add/edit account form with its connection test. Fields are grouped by
// data source: the shared ones (name, source, proxy), the built-in new-api
// ones (site address, credential, user ID, threshold) and the custom-query
// ones (script, placeholder values, threshold).
import type { ComponentChildren } from "preact";
import { useLayoutEffect, useRef, useState } from "preact/hooks";
import { ApiError, createAccount, testConnection, updateAccount } from "./api";
import { CustomTestResult } from "./custom";
import { currencyLabel } from "./format";
import {
  effectiveKind,
  FIELD_ORDER,
  formOf,
  emptyForm,
  needsUserId,
  PROXY_OPTIONS,
  SOURCE_OPTIONS,
  TESTED_FIELDS,
  toInput,
  trimBaseUrl,
  validate,
  type Field,
  type FieldErrors,
  type FormState,
} from "./form-model";
import { t, tDynamic } from "./i18n";
import { Icon } from "./icons";
import type { AccountView, ConnectionTest, Currency } from "./types";

const FIELD_OF: Record<keyof FormState, Field> = {
  name: "name",
  source: "source",
  baseUrl: "base_url",
  credential: "credential",
  userId: "user_id",
  threshold: "low_balance_threshold",
  proxy: "proxy",
  script: "custom_script",
  phBaseUrl: "custom_placeholders.baseUrl",
  apiKey: "custom_placeholders.apiKey",
  accessToken: "custom_placeholders.accessToken",
  phUserId: "custom_placeholders.userId",
};

export const fieldId = (f: Field) => `nm-f-${f}`;

function kindLabel(kind: string): string {
  return t(kind === "pat" ? "credential.pat" : "credential.legacy");
}

function describedBy(...ids: (string | false | undefined)[]): string | undefined {
  const s = ids.filter(Boolean).join(" ");
  return s || undefined;
}

function FieldError(p: { field: Field; reason?: string }) {
  if (!p.reason) return null;
  return (
    <p class="nm-field-error" id={`${fieldId(p.field)}-error`}>
      <Icon name="alert" />
      {tDynamic(`valid.${p.field}.${p.reason}`, tDynamic(`valid.${p.field}.invalid`, p.reason))}
    </p>
  );
}

function TextField(p: {
  field: Field;
  label: string;
  hint?: ComponentChildren;
  hintLive?: boolean;
  error?: string;
  value: string;
  onInput: (v: string) => void;
  onBlur?: () => void;
  type?: "text" | "password";
  inputMode?: "text" | "numeric" | "decimal" | "url";
  placeholder?: string;
  narrow?: boolean;
  mono?: boolean;
}) {
  const id = fieldId(p.field);
  const attrs = {
    id,
    name: p.field,
    class: `nm-input${p.mono ? "" : " nm-input--text"}${p.narrow ? " nm-input--narrow" : ""}`,
    inputMode: p.inputMode,
    autocomplete: "off",
    spellcheck: false,
    placeholder: p.placeholder,
    value: p.value,
    "aria-invalid": p.error ? true : undefined,
    "aria-describedby": describedBy(p.error && `${id}-error`, p.hint !== undefined && `${id}-hint`),
    onBlur: p.onBlur,
  };
  return (
    <div class="nm-field" data-invalid={p.error ? "" : undefined}>
      <label class="nm-field-label" for={id}>
        {p.label}
      </label>
      {/* preact types each input type separately */}
      {p.type === "password" ? (
        <input type="password" {...attrs} onInput={(e) => p.onInput(e.currentTarget.value)} />
      ) : (
        <input type="text" {...attrs} onInput={(e) => p.onInput(e.currentTarget.value)} />
      )}
      <FieldError field={p.field} reason={p.error} />
      {p.hint !== undefined && (
        <p class="nm-field-hint" id={`${id}-hint`} aria-live={p.hintLive ? "polite" : undefined}>
          {p.hint}
        </p>
      )}
    </div>
  );
}

/** The custom-query script editor: monospace, no wrapping, no spell checks. */
function ScriptField(p: { error?: string; value: string; onInput: (v: string) => void }) {
  const id = fieldId("custom_script");
  return (
    <div class="nm-field" data-invalid={p.error ? "" : undefined}>
      <label class="nm-field-label" for={id}>
        {t("form.script")}
      </label>
      <textarea
        id={id}
        name="custom_script"
        class="nm-input nm-script"
        rows={14}
        wrap="off"
        autocomplete="off"
        autocapitalize="off"
        spellcheck={false}
        placeholder={"({\n  request: { url: \"{{baseUrl}}/user/balance\", method: \"GET\", headers: { Authorization: \"Bearer {{apiKey}}\" } },\n  extractor: function (response) {\n    return { remaining: response.balance, unit: \"USD\" };\n  }\n})"}
        value={p.value}
        aria-invalid={p.error ? true : undefined}
        aria-describedby={describedBy(p.error && `${id}-error`, `${id}-hint`, `${id}-risk`)}
        onInput={(e) => p.onInput(e.currentTarget.value)}
      />
      <FieldError field="custom_script" reason={p.error} />
      <p class="nm-field-hint" id={`${id}-hint`}>
        {t("form.script.hint")}
      </p>
      <div class="nm-risk" id={`${id}-risk`} role="note">
        <Icon name="alert" />
        <p>
          <strong>{t("form.script.risk")}</strong> {t("form.script.limits")}
        </p>
      </div>
    </div>
  );
}

function Choice<V extends string>(p: {
  field: Field;
  legend: string;
  value: V;
  options: readonly { value: V; label: string; hint: string }[];
  error?: string;
  onChange: (v: V) => void;
}) {
  const id = fieldId(p.field);
  return (
    <fieldset class="nm-field nm-choices" data-invalid={p.error ? "" : undefined} aria-describedby={p.error ? `${id}-error` : undefined}>
      <legend class="nm-field-label">{p.legend}</legend>
      <div class="nm-choice-row">
        {p.options.map((o, i) => (
          <label key={o.value} class="nm-choice">
            <input
              type="radio"
              name={p.field}
              id={i === 0 ? id : undefined}
              value={o.value}
              checked={p.value === o.value}
              aria-labelledby={`${id}-${o.value}-label`}
              aria-describedby={`${id}-${o.value}-hint`}
              onChange={() => p.onChange(o.value)}
            />
            <span class="nm-choice-text">
              <span class="nm-choice-label" id={`${id}-${o.value}-label`}>
                {o.label}
              </span>
              <span class="nm-choice-hint" id={`${id}-${o.value}-hint`}>
                {o.hint}
              </span>
            </span>
          </label>
        ))}
      </div>
      <FieldError field={p.field} reason={p.error} />
    </fieldset>
  );
}

function failureTitle(code: string): string {
  return tDynamic(`failure.${code}`, code);
}

function Facts(p: { r: ConnectionTest }) {
  const { r } = p;
  return (
    <dl class="nm-facts">
      <div>
        <dt>{t("test.kind")}</dt>
        <dd>{r.credential_kind ? kindLabel(r.credential_kind) : t("test.unknown")}</dd>
      </div>
      <div>
        <dt>{t("test.version")}</dt>
        <dd>{r.site_version || t("test.unknown")}</dd>
      </div>
      <div>
        <dt>{t("test.currency")}</dt>
        <dd>{r.currency ? currencyLabel(r.currency) : t("test.unknown")}</dd>
      </div>
    </dl>
  );
}

export function TestResult(p: { r: ConnectionTest }) {
  const { r } = p;
  if (r.source === "custom") return <CustomTestResult r={r} />;
  if (r.ok || !r.failure) {
    return (
      <div class="nm-test" data-tone="ok">
        <p class="nm-test-title">
          <Icon name="check" />
          {t("test.ok")}
        </p>
        <Facts r={r} />
        {r.data_export === false && <p class="nm-test-text">{t("test.noDashboard")}</p>}
      </div>
    );
  }
  const f = r.failure;
  const partial = r.state === "partial";
  const known = !!(r.site_version || r.currency);
  return (
    <div class="nm-test" data-tone={partial ? "partial" : "error"}>
      <p class="nm-test-title">
        <Icon name="alert" />
        {failureTitle(f.code)}
      </p>
      {partial && <p class="nm-test-text">{t("test.partial")}</p>}
      {partial && r.failures.length > 0 && (
        <ul class="nm-note-list">
          {r.failures.map((pr) => (
            <li key={pr.endpoint}>
              {t("partial.item", { part: tDynamic(`part.${pr.endpoint}`, pr.endpoint), reason: pr.failure ? failureTitle(pr.failure.code) : "" })}
              {pr.failure && <code class="nm-code">{pr.failure.diagnostic}</code>}
            </li>
          ))}
        </ul>
      )}
      <p class="nm-test-text">{tDynamic(`next.${f.code}`, "")}</p>
      {!partial && (
        <p class="nm-test-meta">
          {t("needs.diagnostic")} <code class="nm-code">{f.diagnostic}</code>
        </p>
      )}
      {known && <Facts r={r} />}
    </div>
  );
}

type TestState = { kind: "idle" } | { kind: "running" } | { kind: "done"; result: ConnectionTest };

export function AccountForm(p: {
  apiKey: string;
  /** null: a new account */
  stored: AccountView | null;
  /** the account's display currency when the dashboard knows it */
  currency: Currency | null;
  focusCredential?: boolean;
  onSaved: (a: AccountView) => void;
  onCancel: () => void;
  onUnauthorized: () => void;
}) {
  const { stored } = p;
  const [form, setForm] = useState<FormState>(() => (stored ? formOf(stored) : emptyForm()));
  const [errors, setErrors] = useState<FieldErrors>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [test, setTest] = useState<TestState>({ kind: "idle" });
  const [saving, setSaving] = useState(false);
  const heading = useRef<HTMLHeadingElement>(null);

  useLayoutEffect(() => {
    if (p.focusCredential) document.getElementById(fieldId(form.source === "custom" ? "custom_script" : "credential"))?.focus();
    else heading.current?.focus();
  }, []);

  const focusField = (f: Field) => {
    const el = document.getElementById(fieldId(f)) as HTMLInputElement | null;
    if (!el) return;
    if (el.type === "radio") (document.querySelector<HTMLInputElement>(`input[name="${f}"]:checked`) ?? el).focus();
    else el.focus();
  };

  const focusFirst = (e: FieldErrors) => {
    const first = FIELD_ORDER.find((f) => e[f]);
    if (first) focusField(first);
  };

  const update = <K extends keyof FormState>(k: K, v: FormState[K]) => {
    const next = { ...form, [k]: v };
    setForm(next);
    const field = FIELD_OF[k];
    if (errors[field] || (k === "credential" && errors.user_id)) {
      // Re-check only what is already flagged, so fixing it clears the message.
      const fresh = validate(next, stored);
      setErrors((cur) => {
        const out = { ...cur, [field]: fresh[field] };
        if (cur.user_id) out.user_id = fresh.user_id;
        return out;
      });
    }
    if (TESTED_FIELDS.includes(k) && test.kind === "done") setTest({ kind: "idle" });
  };

  const failMessage = (e: unknown, action: "save" | "test"): FieldErrors | null => {
    if (e instanceof ApiError) {
      if (e.status === 401) {
        p.onUnauthorized();
        return null;
      }
      const body = e.body;
      if (body?.code === "validation_failed" && body.field && FIELD_ORDER.includes(body.field as Field)) {
        return { [body.field as Field]: body.reason ?? "invalid" };
      }
      if (body?.code === "account_not_found") setFormError(t("form.error.notFound"));
      else if (body?.code?.startsWith("data_file_"))
        setFormError(
          t("form.error.dataFile", {
            title: tDynamic(`error.${body.code}.title`, t("error.http.title")),
            text: tDynamic(`error.${body.code}.text`, t("error.http.text", { status: e.status })),
          }),
        );
      else setFormError(t(action === "save" ? "form.error.saveHttp" : "form.error.testHttp", { status: e.status }));
      return null;
    }
    setFormError(t(action === "save" ? "form.error.saveNetwork" : "form.error.testNetwork"));
    return null;
  };

  const runTest = async () => {
    setFormError(null);
    const e = validate(form, stored, true);
    // Name and threshold are not needed to test; keep their flags as they are.
    setErrors((cur) => ({ name: cur.name, low_balance_threshold: cur.low_balance_threshold, ...e }));
    if (Object.keys(e).length > 0) {
      focusFirst(e);
      return;
    }
    setTest({ kind: "running" });
    try {
      const result = await testConnection(p.apiKey, toInput(form, stored), stored?.id);
      setTest({ kind: "done", result });
    } catch (err) {
      setTest({ kind: "idle" });
      const fe = failMessage(err, "test");
      if (fe) {
        setErrors((cur) => ({ ...cur, ...fe }));
        focusFirst(fe);
      }
    }
  };

  const save = async (ev: Event) => {
    ev.preventDefault();
    if (saving) return;
    setFormError(null);
    const e = validate(form, stored);
    setErrors(e);
    if (Object.keys(e).length > 0) {
      focusFirst(e);
      return;
    }
    setSaving(true);
    try {
      const input = toInput(form, stored);
      const saved = stored ? await updateAccount(p.apiKey, stored.id, input) : await createAccount(p.apiKey, input);
      p.onSaved(saved);
    } catch (err) {
      setSaving(false);
      const fe = failMessage(err, "save");
      if (fe) {
        setErrors(fe);
        focusFirst(fe);
      }
    }
  };

  const kind = effectiveKind(form, stored);
  let credentialHint: string;
  if (form.credential.trim()) credentialHint = t(kind === "pat" ? "form.credential.pat" : "form.credential.legacy");
  else if (stored?.credential_masked) credentialHint = t("form.credential.keep", { kind: kindLabel(stored.credential_kind || "legacy") });
  else credentialHint = t("form.credential.hintNew");

  const custom = form.source === "custom";
  const tested = test.kind === "done" ? test.result.currency : null;
  const unit = tested ?? p.currency;
  const thresholdHint = unit
    ? t("form.threshold.hint", { unit: currencyLabel(unit) })
    : t(custom ? "form.threshold.hintScript" : "form.threshold.hintUnknown");
  const thresholdField = (
    <TextField
      field="low_balance_threshold"
      label={t("form.threshold")}
      hint={thresholdHint}
      error={errors.low_balance_threshold}
      inputMode="decimal"
      narrow
      mono
      value={form.threshold}
      onInput={(v) => update("threshold", v)}
    />
  );
  const ph = stored?.source === "custom" ? stored.custom_placeholders : null;
  const secretHint = (typed: string, masked: string | undefined) => (!typed.trim() && masked ? t("form.ph.secret.keep") : t("form.ph.secret.hintNew"));

  return (
    <form class="nm-form" onSubmit={save} noValidate>
      <h2 class="nm-view-title" ref={heading} tabIndex={-1}>
        {stored ? t("form.titleEdit", { name: stored.name }) : t("form.titleNew")}
      </h2>
      {formError && (
        <p class="nm-form-error" role="alert">
          <Icon name="alert" />
          {formError}
        </p>
      )}

      <TextField field="name" label={t("form.name")} hint={t("form.name.hint")} error={errors.name} value={form.name} onInput={(v) => update("name", v)} />

      <Choice
        field="source"
        legend={t("form.source")}
        value={form.source}
        options={SOURCE_OPTIONS.map((o) => ({ value: o.value, label: t(o.label), hint: t(o.hint) }))}
        error={errors.source}
        onChange={(v) => update("source", v)}
      />

      {form.source === "newapi" && (
        <>
          <TextField
            field="base_url"
            label={t("form.baseUrl")}
            hint={t("form.baseUrl.hint")}
            error={errors.base_url}
            inputMode="url"
            placeholder="https://newapi.example.com"
            mono
            value={form.baseUrl}
            onInput={(v) => update("baseUrl", v)}
            onBlur={() => {
              const v = trimBaseUrl(form.baseUrl);
              if (v !== form.baseUrl) update("baseUrl", v);
            }}
          />
          <TextField
            field="credential"
            label={t("form.credential")}
            hint={credentialHint}
            hintLive
            error={errors.credential}
            type="password"
            placeholder={stored?.credential_masked || undefined}
            mono
            value={form.credential}
            onInput={(v) => update("credential", v)}
          />
          {needsUserId(form, stored) && (
            <TextField
              field="user_id"
              label={t("form.userId")}
              hint={t("form.userId.hint")}
              error={errors.user_id}
              inputMode="numeric"
              narrow
              mono
              value={form.userId}
              onInput={(v) => update("userId", v)}
            />
          )}
          {thresholdField}
        </>
      )}

      {custom && (
        <>
          <ScriptField error={errors.custom_script} value={form.script} onInput={(v) => update("script", v)} />
          <fieldset class="nm-field nm-placeholders" aria-describedby="nm-ph-hint">
            <legend class="nm-field-label">{t("form.placeholders")}</legend>
            <p class="nm-field-hint" id="nm-ph-hint">
              {t("form.placeholders.hint")}
            </p>
            <div class="nm-ph-grid">
              <TextField
                field="custom_placeholders.baseUrl"
                label="baseUrl"
                hint={t("form.ph.baseUrl.hint")}
                error={errors["custom_placeholders.baseUrl"]}
                inputMode="url"
                placeholder="https://api.example.com"
                mono
                value={form.phBaseUrl}
                onInput={(v) => update("phBaseUrl", v)}
                onBlur={() => {
                  const v = trimBaseUrl(form.phBaseUrl);
                  if (v !== form.phBaseUrl) update("phBaseUrl", v);
                }}
              />
              <TextField
                field="custom_placeholders.userId"
                label="userId"
                mono
                value={form.phUserId}
                onInput={(v) => update("phUserId", v)}
              />
              <TextField
                field="custom_placeholders.apiKey"
                label="apiKey"
                hint={secretHint(form.apiKey, ph?.apiKeyMasked)}
                hintLive
                type="password"
                placeholder={ph?.apiKeyMasked || undefined}
                mono
                value={form.apiKey}
                onInput={(v) => update("apiKey", v)}
              />
              <TextField
                field="custom_placeholders.accessToken"
                label="accessToken"
                hint={secretHint(form.accessToken, ph?.accessTokenMasked)}
                hintLive
                type="password"
                placeholder={ph?.accessTokenMasked || undefined}
                mono
                value={form.accessToken}
                onInput={(v) => update("accessToken", v)}
              />
            </div>
          </fieldset>
          {thresholdField}
        </>
      )}

      <Choice
        field="proxy"
        legend={t("form.proxy")}
        value={form.proxy}
        options={PROXY_OPTIONS.map((v) => ({ value: v, label: t(`proxy.${v}`), hint: t(`proxy.${v}.hint`) }))}
        error={errors.proxy}
        onChange={(v) => update("proxy", v)}
      />

      <div class="nm-test-slot" role="status" aria-live="polite">
        {test.kind === "running" && <p class="nm-status-line">{t(custom ? "test.script.running" : "test.running")}</p>}
        {test.kind === "done" && <TestResult r={test.result} />}
      </div>

      <div class="nm-form-actions">
        <button type="submit" class="nm-btn nm-btn-primary" disabled={saving} aria-busy={saving}>
          {saving ? t("form.saving") : t(stored ? "form.saveEdit" : "form.saveNew")}
        </button>
        <button type="button" class="nm-btn" onClick={() => void runTest()} disabled={test.kind === "running"} aria-busy={test.kind === "running"}>
          {test.kind === "running" && <Icon name="refresh" class="nm-spin" />}
          {test.kind === "running" ? t(custom ? "form.testingScript" : "form.testing") : t(custom ? "form.testScript" : "form.test")}
        </button>
        <button type="button" class="nm-btn nm-btn-quiet" onClick={p.onCancel}>
          {t("form.cancel")}
        </button>
      </div>
    </form>
  );
}
