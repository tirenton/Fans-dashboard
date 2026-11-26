// Per-member "when are they active" charts for the Members page. Server components; hover tips use
// the global CSS [data-tip] tooltip, so no client JS is needed.
import type { CSSProperties } from "react";
import type { FanActivity, MemberStats } from "@/lib/api";
import { MemberBadge, memberColor } from "@/components/ui";

const pad = (h: number) => String(h).padStart(2, "0");
const plural = (n: number, w: string) => `${n} ${w}${n === 1 ? "" : "s"}`;
const HOUR = 3600e3;

/** "2 likes · 1 comment", leaving out a zero part; "nothing" when both are zero. */
export function likesComments(total: number, comments: number) {
  const likes = total - comments;
  const parts = [likes && plural(likes, "like"), comments && plural(comments, "comment")].filter(Boolean);
  return parts.length ? parts.join(" · ") : "nothing";
}
/** Compact table cell: "2L 1C". */
const lcCell = (total: number, comments: number) =>
  [total - comments ? `${total - comments}L` : "", comments ? `${comments}C` : ""].filter(Boolean).join(" ");

function fmt(iso: string | number, tz: string, opts: Intl.DateTimeFormatOptions) {
  return new Intl.DateTimeFormat("en-GB", { timeZone: tz, hourCycle: "h23", ...opts }).format(new Date(iso));
}

/** Small multiples: one 24-hour bar chart per member, all on the same scale. Comments solid, likes lighter on top;
 * a like counts at its post's time. */
export function ActiveHours({ members, tz, tzLabel }: { members: MemberStats[]; tz: string; tzLabel: string }) {
  const max = Math.max(1, ...members.flatMap((m) => m.hours));
  const late = members.reduce((a, m) => a + (m.late ?? 0), 0);
  return (
    <>
      <div className="hours-grid">
        {members.map((m) => <HoursPanel key={m.member} m={m} max={max} tz={tz} />)}
      </div>
      <div className="legend" style={{ marginTop: 0 }}>
        <span className="tl-key"><span className="sw" style={{ background: "var(--text-secondary)" }} />comments</span>
        <span className="tl-key"><span className="sw" style={{ background: "var(--text-secondary)", opacity: 0.5 }} />likes</span>
      </div>
      <p className="muted small" style={{ marginBottom: 0 }}>
        Likes and comments by hour of day, {tzLabel} time. All panels share one scale (tallest bar ={" "}
        {fmtN(max)} likes and comments). FANS never says when a like happened, so a like counts at its post&apos;s time:
        members like the fresh posts fans make as they arrive.
        {late > 0 && ` ${late} ${late === 1 ? "like is" : "likes are"} Late (found after a check had already seen the post without it), counted at the post's time too.`}
      </p>
      <details className="section">
        <summary>Show as table</summary>
        <div className="table-wrap">
          <table>
            <thead><tr><th>Member</th>{Array.from({ length: 24 }, (_, h) => <th key={h} className="num">{h}</th>)}</tr></thead>
            <tbody>
              {members.map((m) => (
                <tr key={m.member}>
                  <td>{m.member}</td>
                  {m.hours.map((v, h) => <td key={h} className="num">{lcCell(v, m.hoursComments[h])}</td>)}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <p className="muted small">L = likes, C = comments.</p>
      </details>
    </>
  );
}

const fmtN = (v: number) => (Number.isInteger(v) ? String(v) : v >= 10 ? v.toFixed(0) : v.toFixed(1));

function HoursPanel({ m, max, tz }: { m: MemberStats; max: number; tz: string }) {
  const total = m.hours.reduce((a, b) => a + b, 0);
  const comments = m.hoursComments.reduce((a, b) => a + b, 0);
  const peak = total > 0 ? m.hours.indexOf(Math.max(...m.hours)) : -1;
  const style = { "--c": memberColor(m.member) } as CSSProperties;
  const tip = (h: number) => `${m.member} · ${pad(h)}:00–${pad(h)}:59\n${likesComments(m.hours[h], m.hoursComments[h])}`;
  return (
    <div className="hours-panel" style={style}>
      <div className="hours-head">
        <MemberBadge member={m.member} />
        <span className="muted small">
          {total > 0 ? <>{total - comments} likes{m.late ? ` (${m.late} late)` : ""} · {comments} comments</> : "no activity"}
        </span>
      </div>
      <div className="muted small hours-sub">
        {peak >= 0 ? <>Most active <b>{pad(peak)}:00–{pad(peak)}:59</b></> : "No activity yet"}
        {m.lastAt && <> · last {fmt(m.lastAt, tz, { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" })}</>}
      </div>
      <div className="hours-plot" role="img"
        aria-label={`${m.member}: activity by hour of day, ${likesComments(total, comments)}${peak >= 0 ? `, most at ${pad(peak)}:00` : ""}`}>
        {m.hours.map((v, h) => {
          return (
            <div key={h} className={`hours-col${h > 18 ? " tip-end" : h < 4 ? " tip-start" : ""}`} tabIndex={0} data-tip={tip(h)}>
              {v > 0 && (
                <div className="hours-stack" style={{ height: `${(v / max) * 100}%` }}>
                  {v - m.hoursComments[h] > 0 && <div className="hours-bar likes" style={{ flexGrow: v - m.hoursComments[h] }} />}
                  {m.hoursComments[h] > 0 && <div className="hours-bar" style={{ flexGrow: m.hoursComments[h] }} />}
                </div>
              )}
            </div>
          );
        })}
      </div>
      <div className="hours-axis" aria-hidden>
        {[0, 6, 12, 18].map((h) => <span key={h} style={{ left: `${(h / 24) * 100}%` }}>{pad(h)}</span>)}
        <span style={{ right: 0 }}>24</span>
      </div>
    </div>
  );
}

// A cell's count picks its opacity: one hue per row, stronger = more (sequential, in the member's own colour).
const STEPS = [
  { min: 1, label: "1", o: 0.35 },
  { min: 2, label: "2–3", o: 0.55 },
  { min: 4, label: "4–7", o: 0.78 },
  { min: 8, label: "8+", o: 1 },
];
const opacity = (v: number) => [...STEPS].reverse().find((s) => v >= s.min)?.o ?? 0;

type Tick = { at: number; label: string; strong: boolean };
type Mark = { left: string; width: string; tip: string; o?: number };
// which way a mark's tooltip should open, so it is not cut off at either end of the strip
const tipSide = (left: string) => (parseFloat(left) > 70 ? "tip-end" : parseFloat(left) < 15 ? "tip-start" : "");

/**
 * One strip per member, one cell per hour (likes at their post's time). `view` is how many of the API's hours to show,
 * counting back from the current hour: 24 or the full 168. With `month` (its name), the API's hours are one month
 * from its first day, and all of them show.
 */
export function ActivityTimeline({ members, fans, start, hours, view, tz, tzLabel, month }:
  { members: MemberStats[]; fans: FanActivity; start: string; hours: number; view: number; tz: string; tzLabel: string; month?: string }) {
  const off = Math.max(0, hours - view);
  const t0 = Date.parse(start) + off * HOUR, t1 = t0 + view * HOUR;
  const at = (i: number) => t0 + i * HOUR;
  const localHour = (i: number) => Number(fmt(at(i), tz, { hour: "2-digit" }));
  const pctT = (t: number) => `${((t - t0) / (t1 - t0)) * 100}%`;
  const long = view > 240; // a month: label midnights with the day of the month only
  const day = (t: number) => fmt(t, tz, long ? { day: "numeric" } : { weekday: "short", day: "numeric" });

  // gridlines: every 6 h on the 24 h view (midnight labelled with the day), every midnight on the 7 day and month views
  const ticks: Tick[] = [];
  for (let i = 0; i < view; i++) {
    const h = localHour(i);
    if (h === 0) ticks.push({ at: at(i), label: day(at(i)), strong: true });
    else if (view <= 48 && h % 6 === 0) ticks.push({ at: at(i), label: pad(h), strong: false });
  }
  const nearEdge = (t: number) => t1 - t < (view <= 48 ? 1.5 : 14) * HOUR;

  const rows = members.map((m) => {
    const counts = m.timeline.slice(off, off + view);
    const ccounts = m.timelineComments.slice(off, off + view);
    const cells: Mark[] = [];
    counts.forEach((v, i) => {
      if (v > 0) cells.push({
        left: pctT(at(i)), width: pctT(t0 + HOUR), o: opacity(v),
        tip: `${m.member} · ${fmt(at(i), tz, { weekday: "short", day: "numeric", month: "short" })}, ${pad(localHour(i))}:00–${pad(localHour(i))}:59\n${likesComments(v, ccounts[i])}`,
      });
    });
    const timed = counts.reduce((a, b) => a + b, 0);
    const timedC = ccounts.reduce((a, b) => a + b, 0);
    return { m, counts, ccounts, cells, timed, timedC };
  });

  // fan posts per hour, on the same axis: bar height = posts; hours no capture saw are hatched "no data"
  const fanCounts = fans.timeline.slice(off, off + view);
  const fanCov = fans.coverage.slice(off, off + view);
  const fanMax = Math.max(1, ...fanCounts);
  // a run of hours with no data is one hatched band (a month view would otherwise draw hundreds of 1px columns)
  const fanBars: { left: string; width: string; v: number; c: number; tip: string }[] = [];
  const dayHour = (t: number) => fmt(t, tz, { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
  for (let i = 0; i < fanCounts.length; i++) {
    const v = fanCounts[i], c = fanCov[i];
    const when = `${fmt(at(i), tz, { weekday: "short", day: "numeric", month: "short" })}, ${pad(localHour(i))}:00–${pad(localHour(i))}:59`;
    if (c === 0) {
      let j = i + 1;
      while (j < fanCounts.length && fanCov[j] === 0) j++;
      const tip = j - i === 1 ? `Fan posts · ${when}\nno data (the probe missed this hour)`
        : `Fan posts · no data for ${plural(j - i, "hour")}\n${dayHour(at(i))} to ${dayHour(at(j))} (the probe wasn't capturing)`;
      fanBars.push({ left: pctT(at(i)), width: pctT(t0 + (j - i) * HOUR), v: 0, c: 0, tip });
      i = j - 1;
      continue;
    }
    const tip = c < 0.999 ? `Fan posts · ${when}\nat least ${v} (only ${Math.round(c * 100)}% of the hour captured)`
      : `Fan posts · ${when}\n${plural(v, "post")}`;
    fanBars.push({ left: pctT(at(i)), width: pctT(t0 + HOUR), v, c, tip });
  }
  const fanTotal = fanCounts.reduce((a, b) => a + b, 0);
  const fanGaps = fanCov.filter((c) => c === 0).length;

  // table: per-hour columns on the 24 h view, per-day totals on the 7 day view
  const days = ticks.filter((t) => t.strong).map((t) => Math.round((t.at - t0) / HOUR));
  const cols = view <= 48
    ? Array.from({ length: view }, (_, i) => ({ label: pad(localHour(i)), from: i, to: i + 1 }))
    : days.map((d, k) => ({ label: day(at(d)), from: d, to: days[k + 1] ?? view }));
  const rangeLabel = month ?? (view <= 48 ? `Last ${view} hours` : `Last ${Math.round(view / 24)} days`);
  const toNow = t1 > Date.now() - HOUR; // the range runs up to the current hour

  return (
    <>
      <div className="tl-wrap">
        <div className="tl">
          <span />
          <div className="tl-days" aria-hidden>
            {ticks.map((t) => (
              <span key={t.at} className={t.strong ? "strong" : undefined} style={nearEdge(t.at) ? { right: 0 } : { left: pctT(t.at) }}>
                {t.label}
              </span>
            ))}
          </div>
          <span className="tl-name"><span className="badge"><span className="dot" style={{ "--c": "var(--fan)" } as CSSProperties} aria-hidden />Fan posts</span></span>
          <div className="tl-strip tl-fans" style={{ "--c": "var(--fan)" } as CSSProperties} role="img"
            aria-label={`Fan posts, ${rangeLabel.toLowerCase()}: ${plural(fanTotal, "post")} seen, busiest hour ${fanMax}${fanGaps ? `, ${plural(fanGaps, "hour")} with no data` : ""}`}>
            {ticks.map((t) => (
              <span key={t.at} className={`tl-grid${t.strong ? " strong" : ""}`} style={{ left: pctT(t.at) }} aria-hidden />
            ))}
            {fanBars.map((b, i) => (
              <span key={i} className={`tl-fancol ${tipSide(b.left)}`} tabIndex={0} data-tip={b.tip} style={{ left: b.left, width: b.width }}>
                {b.c === 0 ? <span className="nodata-col" />
                  : b.v > 0 && <span className="tl-fanbar" style={{ height: `${(b.v / fanMax) * 100}%`, opacity: b.c < 0.999 ? 0.45 : 1 }} />}
              </span>
            ))}
          </div>
          {rows.map(({ m, cells, timed, timedC }) => (
            <span key={m.member} style={{ display: "contents" }}>
              <span className="tl-name"><MemberBadge member={m.member} /></span>
              <div className="tl-strip" style={{ "--c": memberColor(m.member) } as CSSProperties} role="img"
                aria-label={`${m.member}, ${rangeLabel.toLowerCase()}: ${likesComments(timed, timedC)}`}>
                {ticks.map((t) => (
                  <span key={t.at} className={`tl-grid${t.strong ? " strong" : ""}`} style={{ left: pctT(t.at) }} aria-hidden />
                ))}
                {cells.map((c, k) => (
                  <span key={`c${k}`} className={`tl-cell ${tipSide(c.left)}`} tabIndex={0} data-tip={c.tip}
                    style={{ left: c.left, width: c.width, opacity: c.o }} />
                ))}
              </div>
            </span>
          ))}
        </div>
      </div>
      <div className="legend">
        <span>Likes + comments in an hour:</span>
        {STEPS.map((s) => (
          <span key={s.label} className="tl-key">
            <span className="sw" style={{ background: "var(--text-secondary)", opacity: s.o }} />{s.label}
          </span>
        ))}
        <span className="tl-key"><span className="sw nodata-key" />fan posts: no data</span>
        <span>(each row in the member&apos;s colour)</span>
      </div>
      <p className="muted small" style={{ marginBottom: 0 }}>
        {rangeLabel} in {tzLabel} time{toNow && ", up to the current hour"}. The top row is fan posts per hour (tallest bar ={" "}
        {plural(fanMax, "post")}; faded = only part of the hour was captured, so it&apos;s a minimum). Cells are likes and comments (hover for the split); a like sits at its post&apos;s time.
      </p>
      <details className="section">
        <summary>Show as table</summary>
        <div className="table-wrap">
          <table>
            <thead>
              <tr><th>Member</th>{cols.map((c) => <th key={c.from} className="num">{c.label}</th>)}</tr>
            </thead>
            <tbody>
              <tr>
                <td>Fan posts</td>
                {cols.map((c) => {
                  const cov = fanCov.slice(c.from, c.to);
                  const v = fanCounts.slice(c.from, c.to).reduce((a, b) => a + b, 0);
                  return <td key={c.from} className="num">{cov.every((x) => x === 0) ? "–" : `${v}${cov.some((x) => x < 0.999) ? "+" : ""}`}</td>;
                })}
              </tr>
              {rows.map(({ m, counts, ccounts }) => (
                <tr key={m.member}>
                  <td>{m.member}</td>
                  {cols.map((c) => {
                    const v = counts.slice(c.from, c.to).reduce((a, b) => a + b, 0);
                    const vc = ccounts.slice(c.from, c.to).reduce((a, b) => a + b, 0);
                    return <td key={c.from} className="num">{lcCell(v, vc)}</td>;
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
    </>
  );
}
