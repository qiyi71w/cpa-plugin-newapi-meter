// Everything below a built-in account's title row: the period control, the
// metric band, and usage by model (share bar, charts, table) for the period.
import type { ComponentChildren } from "preact";
import { useEffect, useRef, useState } from "preact/hooks";
import { ApiError, type DetailQuery } from "./api";
import { Chart, type ChartPoint } from "./charts";
import { compact, count, dayLabel, dayTick, fullCount, money, rate, weekday } from "./format";
import { locale, t, tDynamic } from "./i18n";
import { Icon } from "./icons";
import {
  PRESETS,
  bucketParts,
  checkCustom,
  colorOf,
  rangeOf,
  sameChoice,
  seriesOf,
  shapeOf,
  valuesOf,
  type Metric,
  type Range,
} from "./period-model";
import type { AccountDetail, Currency, Failure, OverviewAccount, PeriodChoice, Usage } from "./types";

export function failureTitle(f: Failure | null | undefined): string {
  return f ? tDynamic(`failure.${f.code}`, f.code) : "";
}

/** CPA's today (YYYY-MM-DD) and zone name, from the overview. */
export interface Clock {
  today: string;
  timezone: string;
}

type Trend = "requests" | "tokens";

// ---------- period control ----------

function CustomDates(p: { choice: PeriodChoice; today: string; onApply: (r: Range) => void }) {
  const [draft, setDraft] = useState<Range>(() => rangeOf(p.choice, p.today));
  const [hint, setHint] = useState<{ tone: "info" | "warn"; text: string } | null>(null);
  const change = (field: keyof Range, value: string) => {
    const next = { ...draft, [field]: value };
    const c = checkCustom(next.start, next.end, p.today);
    if (c.ok) {
      setDraft({ start: c.start, end: c.end });
      setHint(c.swapped ? { tone: "info", text: t("period.swapped") } : null);
      p.onApply({ start: c.start, end: c.end });
      return;
    }
    setDraft(next);
    if (c.reason === "too_long") setHint({ tone: "warn", text: t("period.tooLong", { days: c.days ?? 0 }) });
    else if (c.reason === "future") setHint({ tone: "warn", text: t("period.future", { today: dayLabel(p.today, p.today.slice(0, 4)) }) });
    else setHint(null);
  };
  return (
    <div class="nm-custom">
      <label class="nm-date-label">
        {t("period.from")}
        <input
          class="nm-date"
          type="date"
          value={draft.start}
          max={p.today}
          aria-label={t("period.startDate")}
          onChange={(e) => change("start", e.currentTarget.value)}
        />
      </label>
      <label class="nm-date-label">
        {t("period.to")}
        <input
          class="nm-date"
          type="date"
          value={draft.end}
          max={p.today}
          aria-label={t("period.endDate")}
          onChange={(e) => change("end", e.currentTarget.value)}
        />
      </label>
      <p class="nm-period-hint" role="status" data-tone={hint?.tone}>
        {hint?.text}
      </p>
    </div>
  );
}

function PeriodControl(p: { choice: PeriodChoice; onChoice: (c: PeriodChoice) => void; clock: Clock }) {
  const { choice, clock } = p;
  const shape = shapeOf(choice, clock.today);
  const year = clock.today.slice(0, 4);
  const pick = (preset: PeriodChoice["preset"]) => {
    if (preset !== "custom") p.onChoice({ preset });
    // Custom starts from the dates on screen.
    else if (choice.preset !== "custom") p.onChoice({ preset, ...rangeOf(choice, clock.today) });
  };
  return (
    <div class="nm-period">
      <div class="nm-pseg" role="group" aria-label={t("period.label")}>
        {PRESETS.map((k) => (
          <button key={k} type="button" aria-pressed={choice.preset === k} onClick={() => pick(k)}>
            {t(`period.${k}`)}
          </button>
        ))}
      </div>
      {choice.preset === "custom" && (
        <CustomDates choice={choice} today={clock.today} onApply={(r) => p.onChoice({ preset: "custom", ...r })} />
      )}
      <p class="nm-period-note" title={t("period.zone", { zone: clock.timezone })}>
        <span>{shape.start === shape.end ? dayLabel(shape.start, year) : `${dayLabel(shape.start, year)} – ${dayLabel(shape.end, year)}`}</span>
        <span>{t(shape.granularity === "hour" ? "period.hourly" : "period.daily")}</span>
        {shape.includesNow && <span>{t("period.latency")}</span>}
      </p>
    </div>
  );
}

// ---------- metric band ----------

interface Cell {
  key: string;
  label: string;
  value: string;
  title?: string;
  sub?: string;
  tone?: "warn" | "muted";
  pending?: boolean;
  /** first account-level cell: a stronger rule in the one-row band */
  split?: boolean;
}

function unavailable(key: string, label: string, f: Failure | null): Cell {
  return { key, label, value: t("metric.unavailable"), sub: failureTitle(f), tone: "muted" };
}

function balanceCell(a: OverviewAccount): Cell {
  if (a.balance == null) return { ...unavailable("balance", t("metric.balance"), a.balance_failure), split: true };
  const th = a.low_balance_threshold;
  const cell: Cell = { key: "balance", label: t("metric.balance"), value: money(a.balance, a.currency), split: true };
  if (a.state === "partial") return { ...cell, sub: t("metric.thresholdSkipped") };
  if (th == null) return { ...cell, sub: t("metric.noThreshold") };
  const low = a.state === "low";
  return { ...cell, sub: t(low ? "metric.belowThreshold" : "metric.threshold", { amount: money(th, a.currency) }), tone: low ? "warn" : undefined };
}

function accountCells(a: OverviewAccount): Cell[] {
  return [
    balanceCell(a),
    a.total_used == null
      ? unavailable("used", t("metric.totalUsed"), a.balance_failure)
      : { key: "used", label: t("metric.totalUsed"), value: money(a.total_used, a.currency) },
    a.rpm == null && a.tpm == null
      ? unavailable("rt", t("metric.realtime"), a.today_failure)
      : { key: "rt", label: t("metric.realtime"), value: `${rate(a.rpm)} / ${a.tpm == null ? "—" : compact(a.tpm)}`, sub: t("metric.realtimeSub") },
  ];
}

function spendCell(preset: PeriodChoice["preset"], d: AccountDetail | null, a: OverviewAccount): Cell {
  const label = t(`spend.${preset}`);
  if (d) return d.spend_failure ? unavailable("spend", label, d.spend_failure) : { key: "spend", label, value: money(d.spend, d.currency) };
  // Today's figure is already in the overview, from the same query.
  if (preset === "today") return a.today_spend == null ? unavailable("spend", label, a.today_failure) : { key: "spend", label, value: money(a.today_spend, a.currency) };
  return { key: "spend", label, value: "", pending: true };
}

// Averages of quiet accounts are fractions of a request per minute; keep two
// significant digits there instead of rounding them to 0.
const avgSmall = new Intl.NumberFormat(locale, { maximumSignificantDigits: 2 });

function avg(v: number | null): string {
  return v == null ? "—" : v > 0 && v < 10 ? avgSmall.format(v) : rate(v);
}

function usageCells(u: Usage | null): Cell[] {
  return [
    { key: "requests", label: t("metric.requests"), value: u ? count(u.requests) : "", title: u ? fullCount(u.requests) : undefined, pending: !u },
    { key: "tokens", label: t("metric.tokens"), value: u ? compact(u.tokens) : "", title: u ? fullCount(u.tokens) : undefined, pending: !u },
    {
      key: "avg",
      label: t("metric.average"),
      value: u ? `${avg(u.avg_rpm)} / ${u.avg_tpm == null ? "—" : compact(u.avg_tpm)}` : "",
      pending: !u,
    },
  ];
}

function Band(p: { cells: Cell[] }) {
  return (
    <dl class="nm-metrics nm-metrics--period" data-n={p.cells.length}>
      {p.cells.map((c) => (
        <div key={c.key} class="nm-m" data-tone={c.tone} data-split={c.split ? "" : undefined}>
          <dt class="nm-m-label">{c.label}</dt>
          <dd class="nm-m-value" title={c.title ?? (c.pending ? undefined : c.value)}>
            {c.pending ? <span class="nm-skel nm-skel-value" /> : c.value}
          </dd>
          {c.sub && <dd class="nm-m-sub">{c.sub}</dd>}
        </div>
      ))}
    </dl>
  );
}

// ---------- usage ----------

const pct0 = new Intl.NumberFormat(locale, { style: "percent", maximumFractionDigits: 0 });
const pct1 = new Intl.NumberFormat(locale, { style: "percent", minimumFractionDigits: 1, maximumFractionDigits: 1 });

/** Share as a percentage; a sliver that rounds to zero still reads as "<1%". */
function percent(share: number, fmt: Intl.NumberFormat, floor: number): string {
  return share > 0 && share < floor ? `<${fmt.format(floor)}` : fmt.format(share);
}

/** Axis ticks are round numbers, so whole units drop the decimals. */
function tickMoney(c: Currency | null): (v: number) => string {
  if (!c || c.code === "TOKENS") return compact;
  return (v) => (Math.abs(v) >= 1e4 ? `${c.symbol}${compact(v)}` : Number.isInteger(v) ? `${c.symbol}${count(v)}` : money(v, c));
}

function hh(h: number): string {
  return `${String(h).padStart(2, "0")}:00`;
}

function RetryButton(p: { busy: boolean; onRetry: () => void }) {
  return (
    <button type="button" class="nm-btn" onClick={p.onRetry} disabled={p.busy} aria-busy={p.busy}>
      <Icon name="refresh" class={p.busy ? "nm-spin" : undefined} />
      {p.busy ? t("needs.retrying") : t("needs.retry")}
    </button>
  );
}

function PeriodFailure(p: { d: AccountDetail; busy: boolean; onRetry: () => void }) {
  const parts = [
    { key: "spend", label: t("usage.partSpend"), f: p.d.spend_failure },
    { key: "usage", label: t("usage.partUsage"), f: p.d.usage_failure },
  ].filter((x): x is { key: string; label: string; f: Failure } => x.f != null);
  // success:false for a period is how a site refuses a range longer than it allows.
  const rejected = parts.some((x) => x.f.diagnostic === "SUCCESS_FALSE");
  return (
    <div class="nm-note nm-note--failed">
      <p class="nm-note-title">{t("usage.failedTitle")}</p>
      <ul class="nm-note-list">
        {parts.map((x) => (
          <li key={x.key}>
            {t("partial.item", { part: x.label, reason: failureTitle(x.f) })}
            <code class="nm-code">{x.f.diagnostic}</code>
          </li>
        ))}
      </ul>
      <p class="nm-note-text">{rejected ? t("usage.rangeRejected") : tDynamic(`next.${parts[0].f.code}`, "")}</p>
      <div class="nm-actions">
        <RetryButton busy={p.busy} onRetry={p.onRetry} />
      </div>
    </div>
  );
}

function RequestFailure(p: { error: unknown; busy: boolean; onRetry: () => void }) {
  const text = p.error instanceof ApiError ? t("error.http.text", { status: p.error.status }) : t("error.network.text");
  return (
    <div class="nm-note nm-note--failed">
      <p class="nm-note-title">{t("usage.failedTitle")}</p>
      <p class="nm-note-text">{text}</p>
      <div class="nm-actions">
        <RetryButton busy={p.busy} onRetry={p.onRetry} />
      </div>
    </div>
  );
}

function UsageSections(p: { d: AccountDetail; u: Usage; trend: Trend; onTrend: (t: Trend) => void; year: string }) {
  const { d, u, year } = p;
  const cur = d.currency;
  const series = seriesOf(u.models);
  const names = series.map((s) => (s.model == null ? t("usage.others", { n: s.count }) : s.model || t("usage.unnamed")));
  const chartSeries = series.map((s, i) => ({ label: names[i], color: s.color }));
  const hourly = d.period.granularity === "hour";
  const multiDay = d.period.days > 1;
  const pointsOf = (metric: Metric): ChartPoint[] =>
    u.buckets.map((b) => {
      const { date, hour } = bucketParts(b.start);
      return {
        key: b.start,
        tick: hourly ? (multiDay && hour === 0 ? dayTick(date) : hh(hour)) : dayTick(date),
        title: hourly ? `${dayLabel(date, year)} ${hh(hour)}–${hh((hour + 1) % 24)}` : `${dayLabel(date, year)} ${weekday(date)}`,
        values: valuesOf(b, metric, series),
      };
    });
  const tickSteps = hourly ? [1, 2, 3, 4, 6, 12, 24] : [1, 2, 3, 5, 7, 14, 30, 61, 91];
  const spendTip = (v: number) => money(v, cur);
  return (
    <>
      <section class="nm-sec" aria-labelledby="nm-h-share">
        <h3 class="nm-h" id="nm-h-share">
          {t("usage.share")}
        </h3>
        <div class="nm-sharebar" aria-hidden="true">
          {series.map((s, i) => (s.share > 0 ? <span key={i} style={{ flexGrow: s.share, background: s.color }} /> : null))}
        </div>
        <ul class="nm-legend">
          {series.map((s, i) => (
            <li key={i}>
              <i class="nm-swatch" style={{ background: s.color }} />
              <span class="nm-legend-name" title={names[i]}>
                {names[i]}
              </span>
              <b>{percent(s.share, pct0, 0.01)}</b>
              <span class="nm-legend-amt">{money(s.spend, cur)}</span>
            </li>
          ))}
        </ul>
      </section>
      <div class="nm-charts">
        <figure class="nm-chart">
          <figcaption class="nm-chart-head">
            <span class="nm-chart-title">{t("usage.spendChart", { unit: t(hourly ? "period.hourly" : "period.daily") })}</span>
          </figcaption>
          <Chart
            kind="bars"
            points={pointsOf("spend")}
            series={chartSeries}
            format={spendTip}
            tickFormat={tickMoney(cur)}
            label={t("usage.spendChartLabel")}
            totalLabel={t("usage.total")}
            emptyLabel={t("usage.noCalls")}
            tickSteps={tickSteps}
          />
        </figure>
        <figure class="nm-chart">
          <figcaption class="nm-chart-head">
            <span class="nm-chart-title">{t("usage.trend")}</span>
            <span class="nm-pseg nm-pseg--sm" role="group" aria-label={t("usage.trendMetric")}>
              <button type="button" aria-pressed={p.trend === "requests"} onClick={() => p.onTrend("requests")}>
                {t("metric.requests")}
              </button>
              <button type="button" aria-pressed={p.trend === "tokens"} onClick={() => p.onTrend("tokens")}>
                {t("metric.tokens")}
              </button>
            </span>
          </figcaption>
          <Chart
            kind="lines"
            points={pointsOf(p.trend)}
            series={chartSeries}
            format={p.trend === "requests" ? count : compact}
            tickFormat={compact}
            label={t(p.trend === "requests" ? "usage.trendRequests" : "usage.trendTokens")}
            totalLabel={t("usage.total")}
            emptyLabel={t("usage.noCalls")}
            tickSteps={tickSteps}
          />
        </figure>
      </div>
      <section class="nm-sec" aria-labelledby="nm-h-models">
        <h3 class="nm-h" id="nm-h-models">
          {t("usage.table")}
        </h3>
        <div class="nm-tablewrap">
          <table class="nm-usage-table">
            <thead>
              <tr>
                <th scope="col">{t("usage.model")}</th>
                <th scope="col">{t("usage.spend")}</th>
                <th scope="col">{t("usage.shareCol")}</th>
                <th scope="col">{t("metric.requests")}</th>
                <th scope="col">{t("metric.tokens")}</th>
              </tr>
            </thead>
            <tbody>
              {u.models.map((m, i) => {
                const color = colorOf(i, series);
                const name = m.model || t("usage.unnamed");
                return (
                  <tr key={m.model}>
                    <th scope="row">
                      <span class="nm-model">
                        <i class="nm-swatch" style={{ background: color }} />
                        <span class="nm-model-name" title={name}>
                          {name}
                        </span>
                      </span>
                    </th>
                    <td>{money(m.spend, cur)}</td>
                    <td>
                      <span class="nm-sharecell">
                        <span class="nm-sharetrack" aria-hidden="true">
                          <span style={{ width: `${m.share * 100}%`, background: color }} />
                        </span>
                        {percent(m.share, pct1, 0.001)}
                      </span>
                    </td>
                    <td>{count(m.requests)}</td>
                    <td title={fullCount(m.tokens)}>{compact(m.tokens)}</td>
                  </tr>
                );
              })}
            </tbody>
            <tfoot>
              <tr>
                <th scope="row">{t("usage.total")}</th>
                <td>{money(u.spend, cur)}</td>
                <td>{u.spend > 0 ? pct0.format(1) : "—"}</td>
                <td>{count(u.requests)}</td>
                <td title={fullCount(u.tokens)}>{compact(u.tokens)}</td>
              </tr>
            </tfoot>
          </table>
        </div>
      </section>
    </>
  );
}

function UsageArea(p: { d: AccountDetail; trend: Trend; onTrend: (t: Trend) => void; busy: boolean; onRetry: () => void; year: string }) {
  const { d } = p;
  const u = d.usage;
  return (
    <>
      {(d.spend_failure || d.usage_failure) && <PeriodFailure d={d} busy={p.busy} onRetry={p.onRetry} />}
      {d.usage_disabled && (
        <div class="nm-note">
          <p class="nm-note-title">{t("usage.disabledTitle")}</p>
          <p class="nm-note-text">{t("usage.disabledText")}</p>
        </div>
      )}
      {u &&
        (u.models.length === 0 ? (
          <div class="nm-note nm-note--empty">
            <p class="nm-note-title">{t("usage.emptyTitle")}</p>
            <p class="nm-note-text">
              {t("usage.emptyText")}
              {d.period.includes_now && ` ${t("usage.emptyRecent")}`}
            </p>
          </div>
        ) : (
          <UsageSections d={d} u={u} trend={p.trend} onTrend={p.onTrend} year={p.year} />
        ))}
    </>
  );
}

// ---------- the region ----------

interface View {
  /** last detail received; may belong to an earlier choice while the next one loads */
  detail: AccountDetail | null;
  choice: PeriodChoice | null;
  error: { e: unknown; choice: PeriodChoice } | null;
  loading: boolean;
}

export function PeriodDetail(p: {
  a: OverviewAccount;
  clock: Clock;
  choice: PeriodChoice;
  onChoice: (c: PeriodChoice) => void;
  /** bumps after each overview refresh */
  refreshSeq: number;
  loadDetail: (q: DetailQuery) => Promise<AccountDetail>;
  /** today's period is the overview's today query: retrying it refreshes the account */
  retryingToday: boolean;
  onRetryToday: () => void;
  children?: ComponentChildren;
}) {
  const { a, choice, clock } = p;
  const shape = shapeOf(choice, clock.today);
  const isToday = shape.start === clock.today && shape.end === clock.today;
  const [view, setView] = useState<View>({ detail: null, choice: null, error: null, loading: true });
  const [trend, setTrend] = useState<Trend>("requests");
  const seq = useRef(0);
  const seenRefresh = useRef(p.refreshSeq);

  const load = (force: boolean) => {
    const mine = ++seq.current;
    const asked = choice;
    setView((v) => ({ ...v, loading: true }));
    p.loadDetail({ id: a.id, period: asked, force }).then(
      (d) => {
        if (mine === seq.current) setView({ detail: d, choice: asked, error: null, loading: false });
      },
      (e) => {
        if (mine === seq.current) setView((v) => ({ ...v, error: { e, choice: asked }, loading: false }));
      },
    );
  };

  const choiceKey = `${choice.preset}|${choice.start ?? ""}|${choice.end ?? ""}`;
  useEffect(() => {
    // After a refresh, other periods skip the cache; today's was just refreshed by the overview.
    const refreshed = seenRefresh.current !== p.refreshSeq;
    seenRefresh.current = p.refreshSeq;
    load(refreshed && !isToday);
  }, [a.id, choiceKey, p.refreshSeq]);

  const current = view.detail && view.choice && sameChoice(view.choice, choice) ? view.detail : null;
  const error = view.error && sameChoice(view.error.choice, choice) ? view.error.e : null;
  // While another period loads, the previous one stays on screen, dimmed.
  const shown = error && !current ? null : view.detail;
  const stale = !!shown && !current;
  const retry = isToday ? p.onRetryToday : () => load(true);
  const busy = isToday ? p.retryingToday || view.loading : view.loading;

  const cells = [
    spendCell(shown?.period.preset ?? choice.preset, shown, a),
    ...(shown ? (shown.usage ? usageCells(shown.usage) : []) : error ? [] : usageCells(null)),
    ...accountCells(a),
  ];

  return (
    <>
      <PeriodControl choice={choice} onChoice={p.onChoice} clock={clock} />
      <div class="nm-period-body" data-stale={stale && view.loading ? "" : undefined} aria-busy={view.loading}>
        <Band cells={cells} />
        {p.children}
        {error && current && (
          <p class="nm-banner nm-banner--inline" role="status">
            {t("error.refreshFailed")}
          </p>
        )}
        {error && !current ? (
          <RequestFailure error={error} busy={busy} onRetry={retry} />
        ) : shown ? (
          <UsageArea d={shown} trend={trend} onTrend={setTrend} busy={busy} onRetry={retry} year={clock.today.slice(0, 4)} />
        ) : (
          <div class="nm-usage-skel" role="status" aria-label={t("usage.loading")}>
            <span class="nm-skel nm-skel-bar" />
            <div class="nm-charts">
              <span class="nm-skel nm-skel-chart" />
              <span class="nm-skel nm-skel-chart" />
            </div>
          </div>
        )}
      </div>
    </>
  );
}
