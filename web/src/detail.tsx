// Balance-level detail of the selected account.
import { CustomDetail } from "./custom";
import { ago, compact, money, rate, stamp } from "./format";
import { t, tDynamic } from "./i18n";
import { Icon } from "./icons";
import type { Failure, OverviewAccount } from "./types";

export const MANAGE_BUTTON_ID = "nm-manage-open";

/** Failures fixed by a new credential rather than by waiting. */
const CREDENTIAL_FAILURES: Record<string, true> = { legacy_token_retired: true, pat_scope_denied: true, credential_invalid: true };

function failureTitle(f: Failure | null | undefined): string {
  return f ? tDynamic(`failure.${f.code}`, f.code) : "";
}

function RefreshButton(p: { refreshing: boolean; onRefresh: () => void; idle: string; busy: string; hint?: string; primary?: boolean }) {
  return (
    <button
      type="button"
      class={p.primary ? "nm-btn nm-btn-primary" : "nm-btn"}
      onClick={p.onRefresh}
      disabled={p.refreshing}
      aria-busy={p.refreshing}
      title={p.hint}
    >
      <Icon name="refresh" class={p.refreshing ? "nm-spin" : undefined} />
      {p.refreshing ? p.busy : p.idle}
    </button>
  );
}

function Head(p: { a: OverviewAccount; nowMs: number; refreshing: boolean; onRefresh: () => void; onManage: () => void }) {
  const { a } = p;
  // A failing account was still queried just now; when it last succeeded is in the alert.
  const when = a.fetched_at;
  const label = a.state === "needs_action" ? "detail.lastAttempt" : "detail.updated";
  return (
    <header class="nm-head">
      <div class="nm-head-main">
        <div class="nm-title-row">
          <h2 class="nm-title">{a.name}</h2>
          {a.state !== "ok" && (
            <span class="nm-state nm-state--chip" data-state={a.state}>
              <i class="nm-dot" />
              {t(`state.${a.state}`)}
            </span>
          )}
        </div>
        <p class="nm-meta">
          <span>{a.host}</span>
          {a.credential_kind && <span>{t(a.credential_kind === "pat" ? "credential.pat" : "credential.legacy")}</span>}
          {a.site_version && <span>{t("detail.siteVersion", { version: a.site_version })}</span>}
          {when && <span title={stamp(when)}>{t(label, { when: ago(when, p.nowMs) })}</span>}
        </p>
      </div>
      <div class="nm-actions">
        <RefreshButton
          refreshing={p.refreshing}
          onRefresh={p.onRefresh}
          idle={t("detail.refresh")}
          busy={t("detail.refreshing")}
          hint={t("detail.refreshHint")}
        />
        <button type="button" id={MANAGE_BUTTON_ID} class="nm-btn" onClick={p.onManage}>
          {t("manage.open")}
        </button>
      </div>
    </header>
  );
}

interface Cell {
  key: string;
  label: string;
  value: string;
  sub?: string;
  tone?: "warn" | "muted";
}

function unavailable(key: string, label: string, f: Failure | null): Cell {
  return { key, label, value: t("metric.unavailable"), sub: failureTitle(f), tone: "muted" };
}

function balanceCell(a: OverviewAccount): Cell {
  if (a.balance == null) return unavailable("balance", t("metric.balance"), a.balance_failure);
  const th = a.low_balance_threshold;
  const value = money(a.balance, a.currency);
  if (a.state === "partial") return { key: "balance", label: t("metric.balance"), value, sub: t("metric.thresholdSkipped") };
  if (th == null) return { key: "balance", label: t("metric.balance"), value, sub: t("metric.noThreshold") };
  const low = a.state === "low";
  return {
    key: "balance",
    label: t("metric.balance"),
    value,
    sub: t(low ? "metric.belowThreshold" : "metric.threshold", { amount: money(th, a.currency) }),
    tone: low ? "warn" : undefined,
  };
}

function Metrics(p: { a: OverviewAccount }) {
  const { a } = p;
  const cells: Cell[] = [
    balanceCell(a),
    a.total_used == null
      ? unavailable("used", t("metric.totalUsed"), a.balance_failure)
      : { key: "used", label: t("metric.totalUsed"), value: money(a.total_used, a.currency) },
    a.today_spend == null
      ? unavailable("today", t("metric.todaySpend"), a.today_failure)
      : { key: "today", label: t("metric.todaySpend"), value: money(a.today_spend, a.currency) },
    a.rpm == null && a.tpm == null
      ? unavailable("rt", t("metric.realtime"), a.today_failure)
      : { key: "rt", label: t("metric.realtime"), value: `${rate(a.rpm)} / ${a.tpm == null ? "—" : compact(a.tpm)}`, sub: t("metric.realtimeSub") },
  ];
  return (
    <dl class="nm-metrics">
      {cells.map((c) => (
        <div key={c.key} class="nm-m" data-tone={c.tone}>
          <dt class="nm-m-label">{c.label}</dt>
          <dd class="nm-m-value" title={c.value}>
            {c.value}
          </dd>
          {c.sub && <dd class="nm-m-sub">{c.sub}</dd>}
        </div>
      ))}
    </dl>
  );
}

function PartialNote(p: { a: OverviewAccount }) {
  if (p.a.failures.length === 0) return null;
  return (
    <div class="nm-note nm-note--partial" role="note">
      <p class="nm-note-title">{t("partial.title")}</p>
      <ul class="nm-note-list">
        {p.a.failures.map((f) => (
          <li key={f.endpoint}>
            {t("partial.item", { part: tDynamic(`part.${f.endpoint}`, f.endpoint), reason: failureTitle(f.failure) })}
            {f.failure && <code class="nm-code">{f.failure.diagnostic}</code>}
          </li>
        ))}
      </ul>
    </div>
  );
}

function NeedsAction(p: { a: OverviewAccount; refreshing: boolean; onRetry: () => void; onReplaceCredential: () => void }) {
  const { a } = p;
  if (a.unsupported_source) {
    return (
      <div class="nm-alert" role="alert">
        <p class="nm-alert-title">
          <Icon name="alert" />
          {t("unsupported.title")}
        </p>
        <p class="nm-alert-text">{t("unsupported.text")}</p>
      </div>
    );
  }
  const f = a.primary_failure;
  const code = f?.code ?? "bad_response";
  return (
    <div class="nm-alert" role="alert">
      <p class="nm-alert-title">
        <Icon name="alert" />
        {failureTitle(f) || tDynamic(`failure.${code}`, code)}
      </p>
      <p class="nm-alert-text">{tDynamic(`next.${code}`, "")}</p>
      <p class="nm-alert-meta">
        <span>{a.last_success_at ? t("needs.lastSuccess", { when: stamp(a.last_success_at) }) : t("needs.neverSucceeded")}</span>
        {f && (
          <span>
            {t("needs.diagnostic")} <code class="nm-code">{f.diagnostic}</code>
          </span>
        )}
      </p>
      <div class="nm-actions">
        <button type="button" class={CREDENTIAL_FAILURES[code] ? "nm-btn nm-btn-primary" : "nm-btn"} onClick={p.onReplaceCredential}>
          {t("needs.replaceCredential")}
        </button>
        <RefreshButton
          refreshing={p.refreshing}
          onRefresh={p.onRetry}
          idle={t("needs.retry")}
          busy={t("needs.retrying")}
          primary={!CREDENTIAL_FAILURES[code]}
        />
      </div>
    </div>
  );
}

export function Detail(p: {
  a: OverviewAccount;
  nowMs: number;
  refreshing: boolean;
  retrying: boolean;
  onRefresh: () => void;
  onRetry: () => void;
  onManage: () => void;
  onReplaceCredential: () => void;
}) {
  const { a } = p;
  return (
    <>
      <Head a={a} nowMs={p.nowMs} refreshing={p.refreshing} onRefresh={p.onRefresh} onManage={p.onManage} />
      {a.source === "custom" ? (
        <CustomDetail a={a} refreshing={p.retrying || p.refreshing} onRetry={p.onRetry} onEdit={p.onReplaceCredential} />
      ) : a.state === "needs_action" ? (
        <NeedsAction a={a} refreshing={p.retrying || p.refreshing} onRetry={p.onRetry} onReplaceCredential={p.onReplaceCredential} />
      ) : (
        <>
          <Metrics a={a} />
          {a.state === "partial" && <PartialNote a={a} />}
        </>
      )}
    </>
  );
}
