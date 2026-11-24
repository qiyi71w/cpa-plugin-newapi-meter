// Custom-query accounts: the detail below the title row and the test-script
// result. Every string from the script or the service is rendered as text.
import { currencyLabel, money, stamp } from "./format";
import { t, tDynamic } from "./i18n";
import { Icon } from "./icons";
import type { ConnectionTest, Currency, CustomPlan, CustomView, Failure, OverviewAccount, ScriptReason } from "./types";

/** Failures the user fixes by editing the script or its values rather than by waiting. */
const EDIT_FAILURES: Record<string, true> = {
  script_error: true,
  reported_invalid: true,
  credential_invalid: true,
  legacy_token_retired: true,
  pat_scope_denied: true,
};

function failureTitle(f: Failure): string {
  return tDynamic(`failure.${f.code}`, f.code);
}

/** The next step for a custom query; built-in wording where it already fits. */
function nextStep(code: string, diagnostic?: string): string {
  if (diagnostic === "PREVIOUS_QUERY_RUNNING") return t("customNext.busy");
  return tDynamic(`customNext.${code}`, tDynamic(`next.${code}`, ""));
}

/** 「合计」 or 「第一项」 next to a balance made from several plans. */
export function BasisTag(p: { basis: CustomView["balance_basis"] }) {
  if (!p.basis) return null;
  return (
    <span class="nm-basis" title={t(p.basis === "sum" ? "custom.basis.sumHint" : "custom.basis.firstHint")}>
      {t(p.basis === "sum" ? "custom.basis.sum" : "custom.basis.first")}
    </span>
  );
}

function PlanTable(p: { plans: CustomPlan[] }) {
  const { plans } = p;
  // A single plan without a name says nothing the metrics above do not.
  if (plans.length === 0 || (plans.length === 1 && !plans[0].plan_name && !plans[0].extra)) return null;
  return (
    <table class="nm-plan-table">
      <caption class="nm-plan-caption">{t("custom.plans")}</caption>
      <thead>
        <tr>
          <th scope="col">{t("custom.col.plan")}</th>
          <th scope="col">{t("custom.col.remaining")}</th>
          <th scope="col">{t("custom.col.used")}</th>
          <th scope="col">{t("custom.col.total")}</th>
        </tr>
      </thead>
      <tbody>
        {plans.map((pl, i) => (
          <tr key={i} data-invalid={pl.is_valid === false ? "" : undefined}>
            <th scope="row">
              <span class="nm-plan-name">{pl.plan_name || t("custom.planN", { n: i + 1 })}</span>
              {pl.is_valid === false && <span class="nm-plan-flag">{t("custom.planInvalid")}</span>}
              {pl.extra && <span class="nm-plan-extra">{pl.extra}</span>}
            </th>
            <td>{money(pl.remaining, pl.currency)}</td>
            <td>{money(pl.used, pl.currency)}</td>
            <td>{money(pl.total, pl.currency)}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

/** The page's sentence for a failure the plugin explains itself. */
export function reasonText(r: ScriptReason): string {
  const key = r.code === "uncaught" && r.field ? "uncaught_at" : r.code;
  return tDynamic(`custom.reason.${key}`, r.code).replace("{field}", r.field ?? "");
}

/** Why a custom query failed: the plugin's explanation, or the script's own reason or error text, quoted. */
function Reason(p: { view: CustomView | undefined }) {
  const v = p.view;
  if (v?.script_reason) return <p class="nm-quote-text">{reasonText(v.script_reason)}</p>;
  if (v?.script_error) {
    // The title already says the script failed; the engine's text follows as is.
    return <pre class="nm-quote-text nm-quote-text--code">{v.script_error}</pre>;
  }
  if (v?.invalid_message) {
    return (
      <div class="nm-quote">
        <p class="nm-quote-label">{t("test.script.reasonLabel")}</p>
        <p class="nm-quote-text">{v.invalid_message}</p>
      </div>
    );
  }
  return null;
}

interface Cell {
  key: string;
  label: string;
  basis?: CustomView["balance_basis"];
  value: string;
  sub?: string;
  tone?: "warn" | "muted";
}

function amountCell(key: string, label: string, v: number | null, c: Currency | null): Cell {
  return v == null ? { key, label, value: t("custom.notProvided"), tone: "muted" } : { key, label, value: money(v, c) };
}

function Metrics(p: { a: OverviewAccount }) {
  const { a } = p;
  const view = a.custom;
  const th = a.low_balance_threshold;
  const low = a.state === "low";
  const balance = amountCell("balance", t("metric.balance"), a.balance, a.currency);
  balance.basis = view?.balance_basis;
  if (a.balance != null) {
    balance.sub = th == null ? t("metric.noThreshold") : t(low ? "metric.belowThreshold" : "metric.threshold", { amount: money(th, a.currency) });
    if (low) balance.tone = "warn";
  }
  const cells = [
    balance,
    amountCell("used", t("metric.totalUsed"), a.total_used, a.currency),
    amountCell("total", t("custom.totalQuota"), view?.total_quota ?? null, a.currency),
    amountCell("today", t("metric.todaySpend"), a.today_spend, a.today_spend_currency),
  ];
  return (
    <dl class="nm-metrics">
      {cells.map((c) => (
        <div key={c.key} class="nm-m" data-tone={c.tone}>
          <dt class="nm-m-label">
            {c.label}
            <BasisTag basis={c.basis} />
          </dt>
          <dd class="nm-m-value" title={c.value}>
            {c.value}
          </dd>
          {c.sub && <dd class="nm-m-sub">{c.sub}</dd>}
        </div>
      ))}
    </dl>
  );
}

function NeedsAction(p: { a: OverviewAccount; refreshing: boolean; onRetry: () => void; onEdit: () => void }) {
  const { a } = p;
  const f = a.primary_failure;
  const code = f?.code ?? "script_error";
  const edit = !!EDIT_FAILURES[code];
  return (
    <div class="nm-alert" role="alert">
      <p class="nm-alert-title">
        <Icon name="alert" />
        {f ? failureTitle(f) : tDynamic(`failure.${code}`, code)}
      </p>
      <Reason view={a.custom} />
      <p class="nm-alert-text">{nextStep(code, f?.diagnostic)}</p>
      <p class="nm-alert-meta">
        <span>{a.last_success_at ? t("needs.lastSuccess", { when: stamp(a.last_success_at) }) : t("needs.neverSucceeded")}</span>
        {f && (
          <span>
            {t("needs.diagnostic")} <code class="nm-code">{f.diagnostic}</code>
          </span>
        )}
      </p>
      <div class="nm-actions">
        <button type="button" class={edit ? "nm-btn nm-btn-primary" : "nm-btn"} onClick={p.onEdit}>
          {t("custom.edit")}
        </button>
        <button type="button" class={edit ? "nm-btn" : "nm-btn nm-btn-primary"} onClick={p.onRetry} disabled={p.refreshing} aria-busy={p.refreshing}>
          <Icon name="refresh" class={p.refreshing ? "nm-spin" : undefined} />
          {p.refreshing ? t("needs.retrying") : t("needs.retry")}
        </button>
      </div>
    </div>
  );
}

/** Detail of a custom-query account, below the shared title row. */
export function CustomDetail(p: { a: OverviewAccount; refreshing: boolean; onRetry: () => void; onEdit: () => void }) {
  const { a } = p;
  if (a.state === "needs_action") return <NeedsAction a={a} refreshing={p.refreshing} onRetry={p.onRetry} onEdit={p.onEdit} />;
  return (
    <>
      <Metrics a={a} />
      <PlanTable plans={a.custom?.plans ?? []} />
      <p class="nm-custom-note">{t("custom.noPeriod")}</p>
    </>
  );
}

/** The answer of 「测试脚本」. */
export function CustomTestResult(p: { r: ConnectionTest }) {
  const { r } = p;
  const c = r.custom;
  if (r.ok || !r.failure) {
    return (
      <div class="nm-test" data-tone="ok">
        <p class="nm-test-title">
          <Icon name="check" />
          {t("test.script.ok")}
        </p>
        <dl class="nm-facts">
          <div>
            <dt>
              {t("metric.balance")}
              <BasisTag basis={c?.balance_basis} />
            </dt>
            <dd>{c?.balance == null ? t("custom.notProvided") : money(c.balance, r.currency)}</dd>
          </div>
          <div>
            <dt>{t("metric.totalUsed")}</dt>
            <dd>{c?.total_used == null ? t("custom.notProvided") : money(c.total_used, r.currency)}</dd>
          </div>
          <div>
            <dt>{t("custom.totalQuota")}</dt>
            <dd>{c?.total_quota == null ? t("custom.notProvided") : money(c.total_quota, r.currency)}</dd>
          </div>
          <div>
            <dt>{t("metric.todaySpend")}</dt>
            <dd>{c?.today_spend == null ? t("custom.notProvided") : money(c.today_spend, c.today_spend_currency)}</dd>
          </div>
          <div>
            <dt>{t("test.script.unit")}</dt>
            <dd>{r.currency ? currencyLabel(r.currency) : t("test.unknown")}</dd>
          </div>
        </dl>
        <PlanTable plans={c?.plans ?? []} />
      </div>
    );
  }
  const f = r.failure;
  return (
    <div class="nm-test" data-tone="error">
      <p class="nm-test-title">
        <Icon name="alert" />
        {failureTitle(f)}
      </p>
      <Reason view={c} />
      <p class="nm-test-text">{nextStep(f.code, f.diagnostic)}</p>
      <p class="nm-test-meta">
        {t("needs.diagnostic")} <code class="nm-code">{f.diagnostic}</code>
      </p>
    </div>
  );
}
