// Detail of the selected account: the title row, then the period region
// (period.tsx) or the needs-action alert.
import type { DetailQuery } from "./api";
import { ago, stamp } from "./format";
import { t, tDynamic } from "./i18n";
import { Icon } from "./icons";
import { failureTitle, PeriodDetail, type Clock } from "./period";
import type { AccountDetail, OverviewAccount, PeriodChoice } from "./types";

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

function Head(p: { a: OverviewAccount; nowMs: number; refreshing: boolean; onRefresh: () => void }) {
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
      <RefreshButton
        refreshing={p.refreshing}
        onRefresh={p.onRefresh}
        idle={t("detail.refresh")}
        busy={t("detail.refreshing")}
        hint={t("detail.refreshHint")}
      />
    </header>
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

function NeedsAction(p: { a: OverviewAccount; refreshing: boolean; onRetry: () => void }) {
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
        <RefreshButton refreshing={p.refreshing} onRefresh={p.onRetry} idle={t("needs.retry")} busy={t("needs.retrying")} primary />
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
  clock: Clock;
  period: PeriodChoice;
  onPeriod: (c: PeriodChoice) => void;
  refreshSeq: number;
  loadDetail: (q: DetailQuery) => Promise<AccountDetail>;
}) {
  const { a } = p;
  return (
    <>
      <Head a={a} nowMs={p.nowMs} refreshing={p.refreshing} onRefresh={p.onRefresh} />
      {a.state === "needs_action" ? (
        <NeedsAction a={a} refreshing={p.retrying || p.refreshing} onRetry={p.onRetry} />
      ) : (
        <PeriodDetail
          a={a}
          clock={p.clock}
          choice={p.period}
          onChoice={p.onPeriod}
          refreshSeq={p.refreshSeq}
          loadDetail={p.loadDetail}
          retryingToday={p.retrying || p.refreshing}
          onRetryToday={p.onRetry}
        >
          {a.state === "partial" && <PartialNote a={a} />}
        </PeriodDetail>
      )}
    </>
  );
}
