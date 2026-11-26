// The Members page's charts, shared with the public Main dashboard (which adds the fan hour-of-day chart, read-only).
import type { CSSProperties, ReactNode } from "react";
import { api, isMember, MEMBERS, type FanDay, type Stats } from "@/lib/api";
import { ApiDown, Chip, MemberBadge, memberColor, qs } from "@/components/ui";
import { ActiveHours, ActivityTimeline, likesComments } from "@/components/member-time";
import { FanHours } from "@/components/fan-hours";
import { DEFAULT_ZONE, ZONES, zoneLabel, zoneOf } from "@/lib/zones";

const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
// heatmap intensity steps: how much of the member's colour is mixed into the empty-cell colour
const LEVELS = [35, 48, 61, 74, 87, 100];
const mix = (color: string, pct: number) => `color-mix(in srgb, ${color} ${pct}%, var(--surface-2))`;

// tz: the whole page; ttz / wtz: the Activity timeline's and When in the week's own zone (default tz).
// range: 24h, 7d or a month (YYYY-MM) for the timeline; wmonth: a month for When in the week (default all time).
export type MembersSearch = { tz?: string; member?: string; range?: string; ttz?: string; wtz?: string; wmonth?: string };
const RANGES = [
  { id: "24h", label: "Last 24 h", hours: 24 },
  { id: "7d", label: "Last 7 days", hours: 168 },
];
const isMonth = (v?: string): v is string => !!v && /^\d{4}-(0[1-9]|1[0-2])$/.test(v);
/** "Nov 2026" (short) or "November 2026" for YYYY-MM. */
const monthLabel = (ym: string, long = false) =>
  new Intl.DateTimeFormat("en-GB", { timeZone: "UTC", month: long ? "long" : "short", year: "numeric" }).format(new Date(`${ym}-01T00:00:00Z`));
/** Every month (YYYY-MM, in zone) from when captures begin to now. */
function monthsSince(from: string | undefined, zone: string) {
  if (!from) return [];
  const ym = (t: number | string) => {
    const p = new Intl.DateTimeFormat("en-CA", { timeZone: zone, year: "numeric", month: "2-digit" }).formatToParts(new Date(t));
    return [Number(p.find((x) => x.type === "year")!.value), Number(p.find((x) => x.type === "month")!.value)];
  };
  let [y, m] = ym(from);
  const [ny, nm] = ym(Date.now());
  const out: string[] = [];
  while (y < ny || (y === ny && m <= nm)) {
    out.push(`${y}-${String(m).padStart(2, "0")}`);
    if (++m > 12) { m = 1; y++; }
  }
  return out;
}

/** base: the page's own path, for the filter links. fans: also show Fan posts by hour of day (read-only: no
 * links to the locked pages), below the time zone switch. */
export async function MembersView({ sp, base, title, lede, fans }:
  { sp: MembersSearch; base: string; title: string; lede: ReactNode; fans?: boolean }) {
  const tz = zoneOf(sp.tz) ?? DEFAULT_ZONE;
  const ttz = zoneOf(sp.ttz) ?? tz, wtz = zoneOf(sp.wtz) ?? tz;
  const member = isMember(sp.member) ? sp.member : undefined;

  let stats: Stats, tl: Stats, wk: Stats, days: FanDay[] = [];
  let range: { id: string; hours: number; month?: string }, wmonth: string | undefined, tlMonths: string[], wkMonths: string[];
  try {
    stats = await api<Stats>(`/api/stats${qs({ tz, member })}`);
    // each section can show its own zone and month; only months captures have reached are asked for
    tlMonths = monthsSince(stats.dataFrom, ttz);
    wkMonths = monthsSince(stats.dataFrom, wtz);
    range = RANGES.find((r) => r.id === sp.range)
      ?? (isMonth(sp.range) && tlMonths.includes(sp.range) ? { id: sp.range, hours: 0, month: sp.range } : RANGES[0]);
    wmonth = isMonth(sp.wmonth) && wkMonths.includes(sp.wmonth) ? sp.wmonth : undefined;
    [tl, wk, days] = await Promise.all([
      ttz === tz && !range.month ? stats : api<Stats>(`/api/stats${qs({ tz: ttz, month: range.month })}`),
      wtz === tz && !wmonth ? stats : api<Stats>(`/api/stats${qs({ tz: wtz, member, month: wmonth })}`),
      fans ? api<FanDay[]>(`/api/fan-days${qs({ tz })}`).catch(() => []) : Promise.resolve([]),
    ]);
  } catch (e) {
    return (<><h1>{title}</h1><ApiDown error={e} /></>);
  }

  const max = Math.max(1, ...stats.members.flatMap((m) => [m.likes, m.comments]));
  const heatMax = Math.max(0, ...wk.heatmap.flat());
  const level = (v: number) => LEVELS[Math.min(LEVELS.length - 1, Math.floor(((v - 1) / Math.max(1, heatMax)) * LEVELS.length))];
  // each cell in its member's colour; with several members in one hour, split bottom-up by their share
  const byMember = MEMBERS.map((m) => ({ m, grid: wk.heatmapByMember?.[m] })).filter((x) => x.grid && (!member || x.m === member));
  const cellParts = (di: number, h: number) => byMember
    .map(({ m, grid }) => ({ m, v: grid![di][h] }))
    .filter((p) => p.v > 0);
  const shade = (di: number, h: number, v: number) => {
    if (v === 0) return undefined;
    const pct = level(v);
    const parts = cellParts(di, h);
    if (!parts.length) return mix(member ? memberColor(member) : "var(--m-other)", pct);
    const total = parts.reduce((a, p) => a + p.v, 0);
    let acc = 0;
    const stops = parts.flatMap((p) => {
      const c = mix(memberColor(p.m), pct), a = (acc / total) * 100;
      acc += p.v;
      return [`${c} ${a}%`, `${c} ${(acc / total) * 100}%`];
    });
    return stops.length === 2 ? mix(memberColor(parts[0].m), pct) : `linear-gradient(to top, ${stops.join(", ")})`;
  };
  const split = (di: number, h: number) => cellParts(di, h).map((p) => `${p.m} ${p.v}`).join(" · ");
  type Params = { tz?: string; member?: string; range?: string; ttz?: string; wtz?: string; wmonth?: string };
  const href = (p: Params) => `${base}${qs({
    tz: tz === ZONES[0].id ? undefined : tz, member, range: range === RANGES[0] ? undefined : range.id,
    ttz: ttz === tz ? undefined : ttz, wtz: wtz === tz ? undefined : wtz, wmonth, ...p,
  })}`;
  const tzLabel = zoneLabel(tz), ttzLabel = zoneLabel(ttz), wtzLabel = zoneLabel(wtz);

  return (
    <>
      <h1>{title}</h1>
      <p className="lede">{lede}</p>
      <div className="filters">
        <span className="muted small">Times in</span>
        {ZONES.map((z) => (
          <Chip key={z.id} href={href({ tz: z.id === ZONES[0].id ? "" : z.id, ttz: "", wtz: "" })} active={tz === z.id}>{z.label}</Chip>
        ))}
      </div>

      {fans && (
        <section className="card" style={{ marginBottom: 16 }}>
          <h2>Fan posts by hour of day · {tzLabel}</h2>
          <FanHours fans={stats.fans} members={stats.members} days={days} start={stats.timelineStart} tz={tz} tzLabel={tzLabel} readOnly />
        </section>
      )}

      <section className="card">
        <h2>Likes and comments by member</h2>
        <div className="bars">
          <span className="head">Member</span>
          <span className="head">Likes</span>
          <span className="head comments-col">Comments</span>
          {stats.members.map((m) => (
            <Row key={m.member} m={m} max={max} />
          ))}
        </div>
        <p className="muted small" style={{ marginBottom: 0 }}>
          A Late like was found after a check had already seen the post without it: the member came back to an older post.
        </p>
      </section>

      <section className="card section">
        <h2>Active hours by member · {tzLabel}</h2>
        <ActiveHours members={stats.members} tz={tz} tzLabel={tzLabel} />
      </section>

      <section className="card section">
        <h2>Activity timeline · {range.month ? `${monthLabel(range.month, true)} · ` : ""}{ttzLabel}</h2>
        <div className="filters" role="group" aria-label="Timeline range and time zone">
          {RANGES.map((r) => (
            <Chip key={r.id} href={href({ range: r.id === RANGES[0].id ? "" : r.id })} active={range.id === r.id}>{r.label}</Chip>
          ))}
          {tlMonths.length > 0 && <span className="sep" aria-hidden />}
          {tlMonths.map((m) => <Chip key={m} href={href({ range: m })} active={range.month === m}>{monthLabel(m)}</Chip>)}
          <span className="sep" aria-hidden />
          {ZONES.map((z) => <Chip key={z.id} href={href({ ttz: z.id === tz ? "" : z.id })} active={ttz === z.id}>{z.label}</Chip>)}
        </div>
        <ActivityTimeline members={tl.members} fans={tl.fans} start={tl.timelineStart} hours={tl.timelineHours}
          view={range.month ? tl.timelineHours : range.hours} tz={ttz} tzLabel={ttzLabel}
          month={range.month ? monthLabel(range.month, true) : undefined} />
      </section>

      <section className="card section">
        <h2>When in the week · {wmonth ? `${monthLabel(wmonth, true)} · ` : ""}{wtzLabel}{member ? ` · ${member}` : ""}</h2>
        <div className="filters" role="group" aria-label="Month and time zone">
          <Chip href={href({ wmonth: "" })} active={!wmonth}>All time</Chip>
          {wkMonths.map((m) => <Chip key={m} href={href({ wmonth: m })} active={wmonth === m}>{monthLabel(m)}</Chip>)}
          <span className="sep" aria-hidden />
          {ZONES.map((z) => <Chip key={z.id} href={href({ wtz: z.id === tz ? "" : z.id })} active={wtz === z.id}>{z.label}</Chip>)}
        </div>
        <div className="filters">
          <Chip href={href({ member: "" })} active={!member}>All</Chip>
          {MEMBERS.map((m) => (
            <Chip key={m} href={href({ member: m })} active={member === m} color={memberColor(m)}>{m}</Chip>
          ))}
        </div>
        <div className="heat-wrap">
          <div className="heat" role="grid" aria-label={`Artist activity by weekday and hour, ${wtzLabel}${wmonth ? `, ${monthLabel(wmonth, true)}` : ""}`}>
            <span />
            {Array.from({ length: 24 }, (_, h) => (
              <span key={h} className="hh">{h % 3 === 0 ? String(h).padStart(2, "0") : ""}</span>
            ))}
            {DAYS.map((d, di) => (
              <Day key={d} label={d} row={wk.heatmap[di]} comments={wk.heatmapComments[di]}
                shade={(h, v) => shade(di, h, v)} split={(h) => (member ? "" : split(di, h))} />
            ))}
          </div>
        </div>
        <div className="legend">
          <span>Fewer</span>
          <span className="sw" style={{ background: "var(--surface-2)" }} />
          {LEVELS.map((p) => <span key={p} className="sw" style={{ background: mix(member ? memberColor(member) : "var(--text-secondary)", p) }} />)}
          <span>More{heatMax > 0 ? ` (max ${heatMax} likes + comments in an hour)` : ""}</span>
          {!member && <span>· each member in their own colour; an hour with several members is split by their share</span>}
        </div>
        <p className="muted small" style={{ marginBottom: 0 }}>
          Likes are placed at their post&apos;s time (members like fresh posts as fans make them), comments at their exact time.
          {wmonth && ` Only likes and comments in ${monthLabel(wmonth, true)} (${wtzLabel} time).`}
          {" "}L = likes, C = comments.
        </p>
        <details className="section">
          <summary>Show as table</summary>
          <div className="table-wrap">
            <table>
              <thead><tr><th>Day</th>{Array.from({ length: 24 }, (_, h) => <th key={h} className="num">{h}</th>)}</tr></thead>
              <tbody>
                {DAYS.map((d, di) => (
                  <tr key={d}><td>{d}</td>{wk.heatmap[di].map((v, h) => (
                    <td key={h} className="num">{[v - wk.heatmapComments[di][h] ? `${v - wk.heatmapComments[di][h]}L` : "", wk.heatmapComments[di][h] ? `${wk.heatmapComments[di][h]}C` : ""].filter(Boolean).join(" ")}</td>
                  ))}</tr>
                ))}
              </tbody>
            </table>
          </div>
        </details>
      </section>
    </>
  );
}

function Row({ m, max }: { m: Stats["members"][number]; max: number }) {
  const style = { "--c": memberColor(m.member) } as CSSProperties;
  const bar = (v: number, label: string) => (
    <div className="bar-cell" style={style}>
      <span className="bar-value">{v}</span>
      <div className="bar-track" tabIndex={0} data-tip={`${m.member}: ${v} ${label}`}>
        <div className={`bar${v === 0 ? " zero" : ""}`} style={{ width: `${(v / max) * 100}%` }} />
      </div>
    </div>
  );
  return (
    <>
      <MemberBadge member={m.member} />
      {bar(m.likes, m.likes === 1 ? "like" : "likes")}
      <div className="comments-col">{bar(m.comments, m.comments === 1 ? "comment" : "comments")}</div>
    </>
  );
}

function Day({ label, row, comments, shade, split }:
  { label: string; row: number[]; comments: number[]; shade: (h: number, v: number) => string | undefined; split: (h: number) => string }) {
  return (
    <>
      <span className="hl">{label}</span>
      {row.map((v, h) => (
        <span
          key={h}
          // tooltips near either end open inwards, so they aren't cut off at the edge of the chart
          className={`cell${h >= 19 ? " tip-end" : h <= 3 ? " tip-start" : ""}`}
          role="gridcell"
          tabIndex={0}
          style={{ background: shade(h, v) }}
          data-tip={`${label} ${String(h).padStart(2, "0")}:00–${String(h).padStart(2, "0")}:59\n${likesComments(v, comments[h])}${v && split(h) ? `\n${split(h)}` : ""}`}
          aria-label={`${label} ${h}:00, ${likesComments(v, comments[h])}`}
        />
      ))}
    </>
  );
}
