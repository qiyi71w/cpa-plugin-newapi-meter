// The account tab band: one tab per account showing its status, balance,
// today's spend and the headroom gauge; an optional "all accounts" cell sits
// before the tabs and is not a tab.
import { useEffect, useLayoutEffect, useRef, useState } from "preact/hooks";
import { BasisTag } from "./custom";
import { ago, money } from "./format";
import { t, tDynamic } from "./i18n";
import { Icon } from "./icons";
import { gauge, GAUGE_FULL, totals } from "./model";
import type { OverviewAccount } from "./types";

export const tabId = (id: string) => `nm-tab-${id}`;
export const PANEL_ID = "nm-panel";

function failureTitle(code: string | undefined): string {
  return code ? tDynamic(`failure.${code}`, code) : "";
}

function Gauge(p: { a: OverviewAccount }) {
  const g = gauge(p.a);
  const drawn = g.form === "fill" || g.form === "low";
  return (
    <span class="nm-gauge" data-form={g.form} aria-hidden="true">
      {drawn && (
        <>
          <span class="nm-gauge-fill" style={{ width: `${Math.max(3, g.ratio * 100)}%` }} />
          <span class="nm-gauge-tick" style={{ left: `${100 / GAUGE_FULL}%` }} />
        </>
      )}
    </span>
  );
}

function gaugeHint(a: OverviewAccount): string {
  const g = gauge(a);
  if (g.form === "none") return t("gauge.noThreshold");
  if (g.form === "unknown") return a.state === "partial" ? t("gauge.unknown") : "";
  if (g.form === "error") return "";
  const ratio = Number.isFinite(g.multiple) ? g.multiple.toFixed(1) : "∞";
  return t("gauge.ratio", { ratio, threshold: money(a.low_balance_threshold, a.currency) });
}

function Tab(p: { a: OverviewAccount; selected: boolean; nowMs: number; onSelect: (id: string) => void }) {
  const { a } = p;
  let balance;
  let sub: string;
  if (a.unsupported_source) {
    balance = <span class="nm-tab-bal nm-tab-bal--quiet">{t("band.unsupported")}</span>;
    sub = "";
  } else if (a.state === "needs_action") {
    balance = <span class="nm-tab-bal nm-tab-bal--error">{t("band.readFailed")}</span>;
    sub = a.last_success_at ? t("band.lastSuccess", { when: ago(a.last_success_at, p.nowMs) }) : t("band.neverSucceeded");
  } else {
    balance =
      a.balance == null ? (
        <span class="nm-tab-bal nm-tab-bal--quiet">
          {t("band.balanceUnavailable")}
          <small>{failureTitle(a.balance_failure?.code)}</small>
        </span>
      ) : (
        <span class="nm-tab-bal" title={money(a.balance, a.currency)}>
          {a.currency?.code === "UNIT" && a.currency.symbol ? (
            // A script unit can be long: the number keeps the large type, the unit follows small.
            <span class="nm-tab-amt">
              {money(a.balance, { code: "UNIT", symbol: "" })}
              <small class="nm-tab-unit">{a.currency.symbol}</small>
            </span>
          ) : (
            <span class="nm-tab-amt">{money(a.balance, a.currency)}</span>
          )}
        </span>
      );
    sub = t("band.today", { amount: money(a.today_spend, a.currency) });
  }
  const hint = [a.name, t(`state.${a.state}`), gaugeHint(a)].filter(Boolean).join(" · ");
  return (
    <button
      id={tabId(a.id)}
      type="button"
      role="tab"
      class="nm-tab"
      data-state={a.state}
      aria-selected={p.selected}
      aria-controls={PANEL_ID}
      tabIndex={p.selected ? 0 : -1}
      title={hint}
      onClick={() => p.onSelect(a.id)}
    >
      <span class="nm-tab-row">
        <span class="nm-tab-name">{a.name}</span>
        <span class="nm-state" data-state={a.state}>
          <i class="nm-dot" />
          {t(a.state === "low" ? "state.lowShort" : `state.${a.state}`)}
        </span>
      </span>
      {balance}
      <span class="nm-tab-sub">
        {/* 「合计」/「第一项」 sits under the balance so a long amount never loses digits */}
        {a.state !== "needs_action" && a.balance != null && <BasisTag basis={a.custom?.balance_basis} />}
        {sub}
      </span>
      <Gauge a={a} />
    </button>
  );
}

function TotalCell(p: { accounts: OverviewAccount[] }) {
  const tot = totals(p.accounts);
  if (!tot) return null;
  const n = tot.excluded;
  return (
    <div class="nm-total" title={n > 0 ? t("total.excludedHint") : undefined}>
      <span class="nm-total-label">{t("total.label")}</span>
      <span class="nm-tab-bal">{money(tot.balance, tot.currency)}</span>
      <span class="nm-tab-sub">{t("total.today", { amount: money(tot.today, tot.currency) })}</span>
      {n > 0 && <span class="nm-total-note">{t(n === 1 ? "total.excludedOne" : "total.excludedMany", { n })}</span>}
    </div>
  );
}

/** Tracks whether the strip can scroll further left/right. */
function useOverflow(ref: { current: HTMLElement | null }, dep: unknown) {
  const [edges, setEdges] = useState({ l: false, r: false });
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const update = () => {
      const l = el.scrollLeft > 2;
      const r = el.scrollLeft + el.clientWidth < el.scrollWidth - 2;
      setEdges((e) => (e.l === l && e.r === r ? e : { l, r }));
    };
    update();
    el.addEventListener("scroll", update, { passive: true });
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => {
      el.removeEventListener("scroll", update);
      ro.disconnect();
    };
  }, [dep]);
  return edges;
}

export function TabBand(p: { accounts: OverviewAccount[]; selected: string; nowMs: number; onSelect: (id: string) => void }) {
  const { accounts, selected, onSelect } = p;
  const strip = useRef<HTMLDivElement>(null);
  const edges = useOverflow(strip, accounts.length);
  const reduceMotion = typeof matchMedia === "function" && matchMedia("(prefers-reduced-motion: reduce)").matches;

  useEffect(() => {
    document.getElementById(tabId(selected))?.scrollIntoView({
      block: "nearest",
      inline: "nearest",
      behavior: reduceMotion ? "auto" : "smooth",
    });
  }, [selected]);

  const onKeyDown = (e: KeyboardEvent) => {
    const i = accounts.findIndex((a) => a.id === selected);
    const n = accounts.length;
    let next = -1;
    if (e.key === "ArrowRight") next = (i + 1) % n;
    else if (e.key === "ArrowLeft") next = (i - 1 + n) % n;
    else if (e.key === "Home") next = 0;
    else if (e.key === "End") next = n - 1;
    if (next < 0) return;
    e.preventDefault();
    onSelect(accounts[next].id);
    document.getElementById(tabId(accounts[next].id))?.focus({ preventScroll: true });
  };

  const nudge = (dir: number) => {
    const el = strip.current;
    if (el) el.scrollBy({ left: dir * el.clientWidth * 0.7, behavior: reduceMotion ? "auto" : "smooth" });
  };

  return (
    <div class="nm-band">
      <TotalCell accounts={accounts} />
      <div class="nm-scroller">
        <div class="nm-strip" ref={strip} role="tablist" aria-label={t("band.label")} onKeyDown={onKeyDown}>
          {accounts.map((a) => (
            <Tab key={a.id} a={a} selected={a.id === selected} nowMs={p.nowMs} onSelect={onSelect} />
          ))}
        </div>
        {edges.l && (
          <div class="nm-fade nm-fade-l">
            <button type="button" class="nm-nudge" aria-label={t("band.scrollLeft")} title={t("band.scrollLeft")} tabIndex={-1} onClick={() => nudge(-1)}>
              <Icon name="left" />
            </button>
          </div>
        )}
        {edges.r && (
          <div class="nm-fade nm-fade-r">
            <button type="button" class="nm-nudge" aria-label={t("band.scrollRight")} title={t("band.scrollRight")} tabIndex={-1} onClick={() => nudge(1)}>
              <Icon name="right" />
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
