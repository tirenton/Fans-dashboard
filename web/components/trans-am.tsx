"use client";

import { memo, useEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";

// While the probe's Trans-Am runs (a pass about every 30 s for 5 min), the whole site glows red: this sets
// <html data-transam> (styles in globals.css) and shows a badge with the time left. Everyone sees it, so the glow
// says "fast mode is on right now". It asks /api/transam every 20 s, every 5 s while one runs.
type State = { active: boolean; test?: boolean; style?: "advance"; until?: string };

export function TransAmGlow() {
  const [s, setS] = useState<State>({ active: false });
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    let stop = false, timer: ReturnType<typeof setTimeout>;
    const poll = async () => {
      let next: State = { active: false };
      try {
        const r = await fetch("/api/transam", { cache: "no-store" });
        if (r.ok) next = await r.json();
      } catch {}
      if (stop) return;
      setS(next);
      timer = setTimeout(poll, next.active ? 5_000 : 20_000);
    };
    poll();
    const tick = setInterval(() => setNow(Date.now()), 1_000);
    return () => { stop = true; clearTimeout(timer); clearInterval(tick); };
  }, []);

  const until = s.until ? Date.parse(s.until) : NaN;
  // a run whose end has passed stops glowing at once, without waiting for the next answer
  const on = s.active && (!Number.isFinite(until) || until > now - 5_000);

  useEffect(() => {
    const el = document.documentElement;
    if (on) el.dataset.transam = s.test ? "test" : "on";
    else delete el.dataset.transam;
    // the "advance" look: the edge glow beats like a heart (globals.css)
    if (on && s.style === "advance") el.dataset.taStyle = "advance";
    else delete el.dataset.taStyle;
    return () => { delete el.dataset.transam; delete el.dataset.taStyle; };
  }, [on, s.test, s.style]);

  if (!on) return null;
  const left = Number.isFinite(until) ? Math.max(0, Math.ceil((until - now) / 1000)) : null;
  return (
    <>
      <span className="ta-badge" role="status" title={s.test ? "Trans-Am test: pretend passes, no requests to FANS" : "Trans-Am: the probe reads the feed about every 30 seconds"}>
        TRANS-AM{s.test && <span className="ta-test">{s.style === "advance" ? "ADVANCE" : "TEST"}</span>}
        {left !== null && <span className="ta-left">{Math.floor(left / 60)}:{String(left % 60).padStart(2, "0")}</span>}
      </span>
      {/* the side lights and veins go straight into <body>: the top bar's backdrop blur would trap them inside it */}
      {createPortal(s.style === "advance" ? <PageVeins /> : <SideLights />, document.body)}
    </>
  );
}

// Red circuit lines in the empty space left and right of the page (wide screens only, see .ta-sides in globals.css).
// Each side has its own random tangle: traces wander from near the page's edge in 45° and 90° steps of random length,
// some branch, some are hairlines and some thick, and small marks are scattered between them. A faint copy stays lit;
// over it every trace flashes on its own random timing, so the light never settles into a pattern. Drawn here, not
// taken from any picture. A new random tangle each time Trans-Am starts.
const VW = 220, VH = 700; // viewBox
type Trace = { d: string; w: number; delay: number; dur: number; end?: [number, number, number] };
const DIRS: [number, number][] = [[1, 0], [1, -1], [0, -1], [-1, -1], [-1, 0], [-1, 1], [0, 1], [1, 1]];

// inner: the x of the edge next to the page (traces start near it and mostly wander away from it)
function tangle(inner: number): Trace[] {
  const r = Math.random, f = (n: number) => n.toFixed(1);
  const pick = <T,>(a: T[]) => a[Math.floor(r() * a.length)];
  const away = inner === 0 ? 1 : -1; // the x direction pointing away from the page
  const out: Trace[] = [];
  const timing = () => ({ delay: r() * 5, dur: 1.8 + r() * 4.2 });
  const walk = (x: number, y: number, steps: number, w: number, depth: number) => {
    let d = `M${f(x)} ${f(y)}`;
    let dir = pick(DIRS.filter(([dx]) => dx === away || (dx === 0 && r() < 0.5)));
    for (let s = 0; s < steps; s++) {
      if (s > 0) { // turn by 45° or 90°, sometimes keep going; never straight back toward the page for long
        const i = DIRS.indexOf(dir), turn = pick([-2, -1, -1, 0, 0, 1, 1, 2]);
        dir = DIRS[(i + turn + 8) % 8];
        if (dir[0] === -away && r() < 0.6) dir = [away, dir[1]];
      }
      const len = pick([6, 10, 14, 20, 28, 40, 60, 90]) * (0.6 + r() * 0.8);
      x = Math.min(VW - 3, Math.max(3, x + dir[0] * len));
      y = Math.min(VH - 3, Math.max(3, y + dir[1] * len));
      d += ` L${f(x)} ${f(y)}`;
      if (depth < 2 && r() < 0.12) walk(x, y, 1 + Math.floor(r() * 4), Math.max(0.6, w * 0.6), depth + 1); // a branch
    }
    out.push({ d, w, ...timing(), end: r() < 0.7 ? [x, y, pick([2, 3, 3, 4, 6])] : undefined });
  };
  for (let k = 0; k < 34; k++) {
    // most start at the page's edge, some anywhere in the side
    const near = r() < 0.7;
    const x = near ? (inner === 0 ? r() * 18 : VW - r() * 18) : 10 + r() * (VW - 20);
    walk(x, 10 + r() * (VH - 20), 1 + Math.floor(r() * 7), pick([0.6, 0.8, 1, 1.2, 1.6, 2, 2.6, 3.4]), 0);
  }
  for (let k = 0; k < 26; k++) { // scattered marks: little hooks and brackets of random size
    const x = 6 + r() * (VW - 30), y = 6 + r() * (VH - 20), a = 3 + r() * 18, b = 3 + r() * 14;
    const d = pick([`M${f(x)} ${f(y)} h${f(a)} v${f(b)}`, `M${f(x)} ${f(y)} v${f(b)} h${f(a)}`, `M${f(x)} ${f(y)} h${f(a)}`,
      `M${f(x)} ${f(y)} l${f(a)} ${f(-a)} h${f(b)}`, `M${f(x)} ${f(y)} v${f(b)} h${f(a)} v${f(-b / 2)}`]);
    out.push({ d, w: pick([0.6, 1, 1.4, 2]), ...timing() });
  }
  return out;
}

function Circuit({ inner }: { inner: number }) {
  const [traces] = useState(() => tangle(inner)); // made once per Trans-Am: the side lights mount when it starts
  return (
    <svg viewBox={`0 0 ${VW} ${VH}`} preserveAspectRatio={inner === 0 ? "xMinYMid meet" : "xMaxYMid meet"} aria-hidden="true">
      <g className="ta-dim">
        {traces.map((t, i) => <path key={i} d={t.d} style={{ strokeWidth: t.w }} />)}
      </g>
      <g className="ta-lit">
        {traces.map((t, i) => (
          <path key={i} d={t.d} pathLength={1}
            style={{ strokeWidth: t.w, animationDelay: `${t.delay.toFixed(2)}s`, animationDuration: `${t.dur.toFixed(2)}s` }} />
        ))}
        {traces.map((t, i) => t.end && (
          <rect key={`n${i}`} x={t.end[0] - t.end[2] / 2} y={t.end[1] - t.end[2] / 2} width={t.end[2]} height={t.end[2]}
            style={{ animationDelay: `${(t.delay + t.dur * 0.25).toFixed(2)}s`, animationDuration: `${t.dur.toFixed(2)}s` }} />
        ))}
      </g>
    </svg>
  );
}

// The "advance" look: blood vessels behind the whole page instead of circuits beside it. Vessels grow from a grid of
// starting points covering both margins, top to bottom, so no stretch is left bare; each one curves, splits, and thins
// from its root to nothing at its tip (its branches start as thick as it is where they split off, and thin out too),
// so every vessel fades away. They beat at 150 bpm: the vessels flush at each lub and dub, and each beat sends rings of
// light out from the mouse pointer across them (globals.css .ta-veins-lit), lub then dub, fading with distance. The
// drawing scrolls with the page and sits behind it (cards cover it; it shows in the margins and between cards).
// Drawn here, not taken from any picture.
const PAGE_HALF = 560; // half the page column (globals.css .shell max-width 1120px)
const CELL = 115; // one starting point in most cells of this size, across each margin

function network(W: number, H: number): { w: number; h: number; vessels: string[] } {
  const r = Math.random, f = (n: number) => n.toFixed(1);
  const vessels: string[] = [];
  const L: [number, number] = [8, W / 2 - PAGE_HALF - 20], R: [number, number] = [W / 2 + PAGE_HALF + 20, W - 8];
  // a vessel: a chain of gently bending curves (sampled into points), thinning from w0 at its root to 0 at its tip,
  // drawn as one filled tapered outline; branches split off along it, as thick as it is there, and thin out in turn
  const grow = (x: number, y: number, ang: number, w0: number, len: number, depth: number, g: [number, number]) => {
    const pts: [number, number][] = [[x, y]];
    const forks: { i: number; ang: number }[] = [];
    const pieces = 3 + Math.floor(r() * 3);
    for (let p = 0; p < pieces; p++) {
      ang += (r() - 0.5) * 0.7;
      const l = (len / pieces) * (0.7 + r() * 0.6);
      const ex = x + Math.cos(ang) * l, ey = y + Math.sin(ang) * l;
      if (ex < g[0] || ex > g[1] || ey < 4 || ey > H - 4) break;
      const bend = (r() - 0.5) * l * 0.45;
      const cx = (x + ex) / 2 - Math.sin(ang) * bend, cy = (y + ey) / 2 + Math.cos(ang) * bend;
      for (let i = 1; i <= 6; i++) {
        const t = i / 6, u = 1 - t;
        pts.push([u * u * x + 2 * u * t * cx + t * t * ex, u * u * y + 2 * u * t * cy + t * t * ey]);
      }
      if (p < pieces - 1 && depth < 3 && r() < [0.55, 0.4, 0.25][depth]) forks.push({ i: pts.length - 1, ang });
      x = ex; y = ey;
    }
    if (pts.length < 3) return;
    const run = [0];
    for (let i = 1; i < pts.length; i++) run.push(run[i - 1] + Math.hypot(pts[i][0] - pts[i - 1][0], pts[i][1] - pts[i - 1][1]));
    const total = run[run.length - 1];
    const width = (i: number) => w0 * Math.pow(1 - run[i] / total, 0.85);
    const left: string[] = [], right: string[] = [];
    for (let i = 0; i < pts.length; i++) {
      const a = pts[Math.max(0, i - 1)], b = pts[Math.min(pts.length - 1, i + 1)];
      const dx = b[0] - a[0], dy = b[1] - a[1], n = Math.hypot(dx, dy) || 1;
      const h = width(i) / 2, nx = (-dy / n) * h, ny = (dx / n) * h;
      left.push(`${f(pts[i][0] + nx)} ${f(pts[i][1] + ny)}`);
      right.push(`${f(pts[i][0] - nx)} ${f(pts[i][1] - ny)}`);
    }
    vessels.push(`M${left.join(" L")} L${right.reverse().join(" L")}Z`);
    for (const k of forks) {
      const side = r() < 0.5 ? -1 : 1;
      grow(pts[k.i][0], pts[k.i][1], k.ang + side * (0.35 + r() * 0.6), width(k.i) * 0.85, (total - run[k.i]) * (0.6 + r() * 0.4) + 40, depth + 1, g);
    }
  };
  for (const g of [L, R]) {
    const cols = Math.max(1, Math.round((g[1] - g[0]) / CELL)), cw = (g[1] - g[0]) / cols;
    for (let y = 0; y < H; y += CELL) {
      for (let c = 0; c < cols; c++) {
        if (r() < 0.12) continue;
        grow(g[0] + c * cw + r() * cw, y + r() * CELL, r() * Math.PI * 2, 1.8 + r() * 3.4, 90 + r() * 140, 0, g);
      }
    }
  }
  return { w: W, h: H, vessels };
}

// Covers the whole page (it scrolls with it); redrawn when the page's width changes or its height changes a lot.
// Two copies of the vessels: the dim ones, and a bright one that only shows where the rings from the mouse are.
function PageVeins() {
  const [size, setSize] = useState<{ w: number; h: number } | null>(null);
  const lit = useRef<SVGSVGElement>(null);
  useEffect(() => {
    const shell = document.querySelector("main.shell") as HTMLElement | null;
    const measure = () => {
      const w = document.documentElement.clientWidth;
      const h = Math.max(window.innerHeight, shell ? shell.offsetTop + shell.offsetHeight : document.body.scrollHeight);
      setSize((s) => (s && s.w === w && Math.abs(s.h - h) < 120 ? s : { w, h }));
    };
    measure();
    const ro = new ResizeObserver(measure);
    if (shell) ro.observe(shell);
    window.addEventListener("resize", measure);
    return () => { ro.disconnect(); window.removeEventListener("resize", measure); };
  }, []);
  // the rings' centre: the mouse pointer, in page coordinates (it moves on the page as you scroll, too)
  useEffect(() => {
    let cx = window.innerWidth / 2, cy = window.innerHeight / 2;
    const place = () => {
      lit.current?.style.setProperty("--mx", `${Math.round(cx + window.scrollX)}px`);
      lit.current?.style.setProperty("--my", `${Math.round(cy + window.scrollY)}px`);
    };
    const move = (e: PointerEvent) => { cx = e.clientX; cy = e.clientY; place(); };
    place();
    window.addEventListener("pointermove", move, { passive: true });
    window.addEventListener("scroll", place, { passive: true });
    return () => { window.removeEventListener("pointermove", move); window.removeEventListener("scroll", place); };
  }, [size]);
  const net = useMemo(() => (size && size.w >= 1360 ? network(size.w, size.h) : null), [size]);
  if (!net) return null;
  const box = { width: net.w, height: net.h, viewBox: `0 0 ${net.w} ${net.h}`, "aria-hidden": true } as const;
  return (
    <>
      <svg className="ta-page-veins" {...box}>
        <g className="ta-vessels">{net.vessels.map((d, i) => <path key={i} d={d} />)}</g>
      </svg>
      <svg className="ta-page-veins ta-veins-lit" ref={lit} {...box}>
        {net.vessels.map((d, i) => <path key={i} d={d} />)}
      </svg>
    </>
  );
}

const SideLights = memo(function SideLights() {
  return (
    <div className="ta-sides" aria-hidden="true">
      <div className="ta-side ta-side-left"><Circuit inner={VW} /></div>
      <div className="ta-side ta-side-right"><Circuit inner={0} /></div>
    </div>
  );
});
