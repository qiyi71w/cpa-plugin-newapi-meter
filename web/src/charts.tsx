// Self-drawn SVG charts over the period's buckets: stacked bars and lines.
// The hover tooltip is measured and kept inside the chart box.
import type { RefObject } from "preact";
import { useLayoutEffect, useRef, useState } from "preact/hooks";

export interface ChartPoint {
  key: string;
  /** x-axis label */
  tick: string;
  /** tooltip heading */
  title: string;
  /** one value per series */
  values: number[];
}

export interface ChartSeries {
  label: string;
  color: string;
}

interface ChartProps {
  kind: "bars" | "lines";
  points: ChartPoint[];
  series: ChartSeries[];
  /** tooltip values */
  format: (v: number) => string;
  /** y-axis labels; defaults to format */
  tickFormat?: (v: number) => string;
  /** accessible name of the chart */
  label: string;
  totalLabel: string;
  emptyLabel: string;
  /** allowed label intervals, in points (e.g. every 1, 2, 3, 6 hours) */
  tickSteps: number[];
  height?: number;
}

const PAD = { r: 4, t: 8, b: 24 };

function useWidth<T extends HTMLElement>(): [RefObject<T | null>, number] {
  const ref = useRef<T>(null);
  const [w, setW] = useState(0);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(([e]) => setW(Math.floor(e.contentRect.width)));
    ro.observe(el);
    setW(Math.floor(el.getBoundingClientRect().width));
    return () => ro.disconnect();
  }, []);
  return [ref, w];
}

/** About four intervals on a 1/2/5×10^k step, so tick labels are round. */
function niceScale(v: number): { top: number; ticks: number[] } {
  if (!(v > 0)) return { top: 1, ticks: [0, 1] };
  const raw = v / 4;
  const p = Math.pow(10, Math.floor(Math.log10(raw)));
  const n = raw / p;
  const step = (n <= 1 ? 1 : n <= 2 ? 2 : n <= 5 ? 5 : 10) * p;
  const count = Math.max(1, Math.ceil(v / step - 1e-9));
  const ticks = Array.from({ length: count + 1 }, (_, i) => +(i * step).toPrecision(12));
  return { top: ticks[count], ticks };
}

export function Chart(p: ChartProps) {
  const height = p.height ?? 216;
  const [box, w] = useWidth<HTMLDivElement>();
  const tip = useRef<HTMLDivElement>(null);
  const [hover, setHover] = useState<number | null>(null);
  const n = p.points.length;

  const peak = Math.max(0, ...p.points.flatMap((pt) => (p.kind === "bars" ? [pt.values.reduce((a, b) => a + b, 0)] : pt.values)));
  const { top, ticks } = niceScale(peak);
  const tickLabels = ticks.map((v) => (p.tickFormat ?? p.format)(v));
  // Room for the longest y label (11px digits are ~6.5px wide).
  const padL = Math.round(Math.min(76, Math.max(28, Math.max(...tickLabels.map((l) => l.length)) * 6.5 + 10)));
  const innerW = Math.max(0, w - padL - PAD.r);
  const innerH = height - PAD.t - PAD.b;
  const band = n ? innerW / n : 0;
  const xOf = (i: number) => padL + band * i + band / 2;
  const yOf = (v: number) => PAD.t + innerH * (1 - v / top);

  // Keep the tooltip inside the box: beside the hovered point, flipped left near the right edge.
  useLayoutEffect(() => {
    const el = tip.current;
    if (!el || hover == null) return;
    const tw = el.offsetWidth;
    const th = el.offsetHeight;
    const x = xOf(hover);
    let left = x + 12;
    if (left + tw > w) left = x - 12 - tw;
    left = Math.max(0, Math.min(left, w - tw));
    el.style.left = `${Math.round(left)}px`;
    el.style.top = `${Math.round(Math.max(0, Math.min(PAD.t, height - th)))}px`;
    el.style.visibility = "visible";
  });

  const fromPointer = (e: PointerEvent) => {
    const r = box.current?.getBoundingClientRect();
    if (!r || !band) return;
    const i = Math.floor((e.clientX - r.left - padL) / band);
    setHover(i >= 0 && i < n ? i : null);
  };

  const onKeyDown = (e: KeyboardEvent) => {
    if (!n) return;
    const cur = hover ?? n - 1;
    let next = -1;
    if (e.key === "ArrowRight") next = Math.min(n - 1, cur + 1);
    else if (e.key === "ArrowLeft") next = Math.max(0, cur - 1);
    else if (e.key === "Home") next = 0;
    else if (e.key === "End") next = n - 1;
    else if (e.key === "Escape") return setHover(null);
    if (next < 0) return;
    e.preventDefault();
    setHover(next);
  };

  const maxTicks = Math.max(1, Math.floor(innerW / 52));
  const every = p.tickSteps.find((s) => Math.ceil(n / s) <= maxTicks) ?? Math.ceil(n / maxTicks);

  const hovered = hover != null ? p.points[hover] : null;
  const rows = hovered
    ? hovered.values
        .map((v, i) => ({ v, i }))
        .filter((r) => r.v > 0)
        .sort((a, b) => b.v - a.v)
    : [];

  return (
    <div
      ref={box}
      class="nm-chart-box"
      style={{ height: `${height}px` }}
      tabIndex={n ? 0 : -1}
      aria-label={p.label}
      onPointerMove={fromPointer}
      onPointerDown={fromPointer}
      onPointerLeave={() => setHover(null)}
      onKeyDown={onKeyDown}
      onBlur={() => setHover(null)}
    >
      {w > 0 && (
        <svg width={w} height={height} aria-hidden="true" focusable="false">
          {ticks.map((v, i) => (
            <g key={v}>
              <line class={i === 0 ? "nm-axis-base" : "nm-axis-grid"} x1={padL} x2={w - PAD.r} y1={yOf(v)} y2={yOf(v)} />
              <text class="nm-axis-text" x={padL - 8} y={yOf(v) + 4} text-anchor="end">
                {tickLabels[i]}
              </text>
            </g>
          ))}
          {p.points.map((pt, i) => {
            if (i % every !== 0) return null;
            // Keep end labels inside the box.
            const half = pt.tick.length * 3.4;
            const x = Math.max(padL + half, Math.min(xOf(i), w - PAD.r - half));
            return (
              <text key={pt.key} class="nm-axis-text" x={x} y={height - 7} text-anchor="middle">
                {pt.tick}
              </text>
            );
          })}
          {hover != null && p.kind === "bars" && <rect class="nm-chart-hover" x={padL + band * hover} y={PAD.t} width={band} height={innerH} />}
          {p.kind === "bars" ? <Bars {...p} band={band} xOf={xOf} innerH={innerH} top={top} /> : <Lines {...p} xOf={xOf} yOf={yOf} />}
          {hover != null && p.kind === "lines" && (
            <g>
              <line class="nm-chart-cursor" x1={xOf(hover)} x2={xOf(hover)} y1={PAD.t} y2={PAD.t + innerH} />
              {p.series.map((s, k) => (
                <circle key={k} cx={xOf(hover)} cy={yOf(p.points[hover].values[k] ?? 0)} r="3.5" fill="var(--nm-raised)" stroke={s.color} stroke-width="2" />
              ))}
            </g>
          )}
        </svg>
      )}
      {hovered && (
        <div ref={tip} class="nm-tooltip" role="status" style={{ visibility: "hidden" }}>
          <p class="nm-tt-title">{hovered.title}</p>
          {rows.map((r) => (
            <p key={r.i} class="nm-tt-row">
              <span class="nm-tt-key">
                <i class="nm-swatch" style={{ background: p.series[r.i].color }} />
                <span class="nm-tt-name">{p.series[r.i].label}</span>
              </span>
              <b>{p.format(r.v)}</b>
            </p>
          ))}
          {rows.length > 1 && (
            <p class="nm-tt-row nm-tt-total">
              <span class="nm-tt-key">{p.totalLabel}</span>
              <b>{p.format(rows.reduce((a, r) => a + r.v, 0))}</b>
            </p>
          )}
          {rows.length === 0 && <p class="nm-tt-row nm-tt-empty">{p.emptyLabel}</p>}
        </div>
      )}
    </div>
  );
}

function Bars(p: ChartProps & { band: number; xOf: (i: number) => number; innerH: number; top: number }) {
  const barW = Math.max(1, Math.min(28, p.band * 0.64));
  const rx = Math.min(1, barW / 4);
  return (
    <g>
      {p.points.map((pt, i) => {
        let y = PAD.t + p.innerH;
        return (
          <g key={pt.key}>
            {pt.values.map((v, k) => {
              if (v <= 0) return null;
              const h = (v / p.top) * p.innerH;
              y -= h;
              // A 1px gap separates stacked segments when there is room for it.
              return <rect key={k} x={p.xOf(i) - barW / 2} y={y} width={barW} height={Math.max(0.75, h > 2 ? h - 1 : h)} rx={rx} fill={p.series[k].color} />;
            })}
          </g>
        );
      })}
    </g>
  );
}

function Lines(p: ChartProps & { xOf: (i: number) => number; yOf: (v: number) => number }) {
  const n = p.points.length;
  if (!n) return null;
  return (
    <g>
      {p.series.map((s, k) => {
        const pts = p.points.map((pt, i) => `${p.xOf(i).toFixed(1)},${p.yOf(pt.values[k] ?? 0).toFixed(1)}`);
        return (
          <g key={k}>
            {p.series.length === 1 && n > 1 && (
              <path d={`M${p.xOf(0).toFixed(1)},${p.yOf(0).toFixed(1)} L${pts.join(" L")} L${p.xOf(n - 1).toFixed(1)},${p.yOf(0).toFixed(1)} Z`} fill={s.color} opacity="0.12" />
            )}
            {n > 1 ? (
              <polyline points={pts.join(" ")} fill="none" stroke={s.color} stroke-width="2" stroke-linejoin="round" stroke-linecap="round" />
            ) : (
              <circle cx={p.xOf(0)} cy={p.yOf(p.points[0].values[k] ?? 0)} r="3" fill={s.color} />
            )}
          </g>
        );
      })}
    </g>
  );
}
