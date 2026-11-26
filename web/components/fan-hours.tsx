"use client";
// Fan posts by hour of day (Fan feed page): an Average and a Past 24 hours chart on one scale. Double-click (or
// Enter on) an hour to see that hour minute by minute; the minutes come from /api/fan-minutes on the web server.
import { createContext, useContext, useEffect, useState, type CSSProperties, type ReactNode } from "react";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import type { FanActivity, FanDay, FanDayHours, FanMinutes, MemberStats } from "@/lib/api";
import { memberColor, qs } from "@/components/ui";

const pad = (h: number) => String(h).padStart(2, "0");
const plural = (n: number, w: string) => `${n} ${w}${n === 1 ? "" : "s"}`;
const HOUR = 3600e3;
function fmt(iso: string | number, tz: string, opts: Intl.DateTimeFormatOptions) {
  return new Intl.DateTimeFormat("en-GB", { timeZone: tz, hourCycle: "h23", ...opts }).format(new Date(iso));
}

const avgFmt = (v: number) => (v >= 10 ? v.toFixed(0) : v.toFixed(1));

type HourCol = { v: number; p: number; kind: "full" | "partial" | "none"; tip: string }; // v posts, p different fans

/** One 24-column bar chart, 00–23, for the fan hour-of-day charts. `now` marks the hour in progress. Double-click
 * (or Enter on) an hour with data calls onZoom. */
function FanHourPlot({ cols, max, label, now, onZoom, strip }:
  { cols: HourCol[]; max: number; label: string; now?: number; onZoom: (h: number) => void; strip?: ReactNode }) {
  return (
    <>
      <div className="hours-plot fan-plot" role="group" style={{ "--c": "var(--fan)" } as CSSProperties} aria-label={label}>
        {cols.map((c, h) => (
          <div key={h} className={`hours-col${h > 18 ? " tip-end" : h < 4 ? " tip-start" : ""}${h === now ? " now-col" : ""}${c.kind !== "none" ? " zoomable" : ""}`}
            tabIndex={0} data-tip={c.kind !== "none" ? `${c.tip}\ndouble-click for minute by minute` : c.tip}
            aria-label={c.tip.replace(/\n/g, ", ")}
            onDoubleClick={() => c.kind !== "none" && onZoom(h)}
            onKeyDown={(e) => { if (e.key === "Enter" && c.kind !== "none") onZoom(h); }}>
            {c.kind === "none" ? <div className="nodata-col" />
              : c.v > 0 && (
                <div className={`hours-bar fan-posts${c.kind === "partial" ? " partial" : ""}`} style={{ height: `${(c.v / max) * 100}%` }}>
                  {c.p > 0 && <div className="fan-people" style={{ height: `${Math.min(1, c.p / c.v) * 100}%` }} />}
                </div>
              )}
          </div>
        ))}
      </div>
      {strip}
      <div className="hours-axis" aria-hidden>
        {[0, 6, 12, 18].map((h) => <span key={h} style={{ left: `${(h / 24) * 100}%` }}>{pad(h)}</span>)}
        <span style={{ right: 0 }}>24</span>
        {now !== undefined && <span className="now-label" style={{ left: `${((now + 0.5) / 24) * 100}%` }}>now</span>}
      </div>
    </>
  );
}

/** One cell of the artist strip: likes and comments per member, and the link listing those posts. */
type ArtistCell = { by: Record<string, number>; when: string; href?: string };
const iso = (t: number) => new Date(t).toISOString();
/** The Fan feed's artist view, filtered to a real time range, or to an hour (and minute) of day on any day. */
const activityRange = (from: number, ms: number, tz: string) => `/feed${qs({ artist: "1", from: iso(from), to: iso(from + ms), tz })}`;
const activityAt = (tz: string, hour: number, minute?: number) =>
  `/feed${qs({ artist: "1", hour: String(hour), minute: minute === undefined ? undefined : String(minute), tz })}`;
const openActivity = (href?: string) => { if (href) window.open(href, "_blank", "noopener"); };
/** false on the public Main dashboard: strip cells don't open the Fan feed (a page that needs a login). */
const LinksOn = createContext(true);
const cellTotal = (c: ArtistCell) => Object.values(c.by).reduce((a, b) => a + b, 0);
const byText = (by: Record<string, number>) =>
  Object.entries(by).filter(([, n]) => n > 0).sort((a, b) => b[1] - a[1]).map(([m, n]) => `${m} ${n}`).join(", ");

/** A heat strip under a chart, aligned with its columns: when artists were active. Each cell is split by member
 * (their colours, biggest share at the bottom); stronger = more likes and comments. Timed events only. */
function ArtistStrip({ cells, note, label }: { cells: ArtistCell[]; note: string; label: string }) {
  const max = Math.max(1, ...cells.map(cellTotal));
  const n = cells.length;
  const links = useContext(LinksOn);
  return (
    <div className="artist-strip" role="img" aria-label={label} style={{ "--n": n } as CSSProperties}>
      {cells.map((c, i) => {
        const t = cellTotal(c);
        const parts = Object.entries(c.by).filter(([, v]) => v > 0).sort((a, b) => b[1] - a[1]);
        const open = links && t > 0 && !!c.href;
        const tip = t ? `${c.when}\nartists: ${t} (${byText(c.by)})\n${note}${open ? "\ndouble-click to list them (new tab)" : ""}`
          : `${c.when}\nno artist activity seen`;
        return (
          <div key={i} className={`artist-cell${open ? " zoomable" : ""}${i > n * 0.8 ? " tip-end" : i < n * 0.15 ? " tip-start" : ""}`}
            data-tip={tip} tabIndex={open ? 0 : undefined} aria-label={open ? tip.replace(/\n/g, ", ") : undefined}
            onDoubleClick={() => open && openActivity(c.href)}
            onKeyDown={(e) => { if (e.key === "Enter" && open) openActivity(c.href); }}>
            {t > 0 && (
              <div className="artist-heat" style={{ opacity: 0.3 + 0.7 * (t / max) }}>
                {parts.map(([m, v]) => <span key={m} style={{ flexGrow: v, background: memberColor(m) }} />)}
              </div>
            )}
          </div>
        );
      })}
    </div>
  );
}

/** "22:00 (12), 21:00 (8)": the strip's busiest cells, for the summary line. */
function topCells(cells: ArtistCell[], name: (i: number) => string, k = 3) {
  return cells.map((c, i) => ({ i, t: cellTotal(c) })).filter((x) => x.t > 0).sort((a, b) => b.t - a.t).slice(0, k)
    .map((x) => `${name(x.i)} (${x.t})`).join(", ");
}

type Zoom = { chart: "avg" | "day"; h: number; at?: number };

/** One hour, minute by minute (60 columns), on its own scale. Minutes no capture saw are striped; partly seen ones
 * faded. For an average, the numbers are per-minute averages over the hours the average uses. */
function MinuteView({ zoom, tz, onBack, onPick }: { zoom: Zoom; tz: string; onBack: () => void; onPick?: (from: number) => void }) {
  const [data, setData] = useState<FanMinutes | null>(null);
  const [error, setError] = useState<string | null>(null);
  useEffect(() => {
    let stop = false;
    setData(null); setError(null);
    const p = new URLSearchParams({ tz });
    if (zoom.chart === "day" && zoom.at) p.set("at", new Date(zoom.at).toISOString());
    else p.set("hour", String(zoom.h));
    fetch(`/api/fan-minutes?${p}`, { cache: "no-store" })
      .then(async (r) => { if (!r.ok) throw new Error(`HTTP ${r.status}`); return (await r.json()) as FanMinutes; })
      .then((d) => { if (!stop) setData(d); })
      .catch((e) => { if (!stop) setError(String(e.message ?? e)); });
    return () => { stop = true; };
  }, [zoom, tz]);

  const avg = zoom.chart === "avg";
  const n = (v: number) => (avg ? avgFmt(v) : String(v));
  const back = <button type="button" className="chip" onClick={onBack}>← All 24 hours</button>;
  if (error) return <div className="muted small">{back} Couldn&apos;t load the minutes ({error}).</div>;
  if (!data) return <div className="muted small">{back} Loading {span(zoom.h)}…</div>;

  const max = Math.max(1, ...data.posts);
  // real hours only: double-click a minute with posts to list them in the feed below
  const t0 = data.start ? Date.parse(data.start) : 0;
  const pickable = (m: number) => !!onPick && !!t0 && data.posts[m] > 0;
  const pick = (m: number) => { if (pickable(m)) onPick!(t0 + m * 60e3); };
  const total = data.posts.reduce((a, b) => a + b, 0);
  const peak = data.posts.indexOf(Math.max(...data.posts));
  const unseen = data.coverage.filter((c) => c < 0.001).length;
  const artistCells: ArtistCell[] = Array.from({ length: 60 }, (_, m) => ({
    by: {}, when: `${pad(zoom.h)}:${pad(m)}`,
    href: avg ? activityAt(tz, zoom.h, m) : t0 ? activityRange(t0 + m * 60e3, 60e3, tz) : undefined,
  }));
  for (const e of data.artist ?? []) artistCells[e.minute].by[e.member] = (artistCells[e.minute].by[e.member] ?? 0) + 1;
  const artistTop = topCells(artistCells, (m) => `:${pad(m)}`);
  const artistNote = avg ? "on any day at this hour (likes and comments)" : "likes and comments";
  return (
    <>
      <div className="muted small hours-sub fan-zoom-head">
        {back}
        <span>
          <b>{span(zoom.h)}</b>
          {avg ? ` · average of ${plural(data.hours, "hour")}` : data.start ? ` · ${fmt(data.start, tz, { weekday: "short", day: "numeric", month: "short" })}` : ""}
          {" · "}{avg ? `about ${avgFmt(total)}` : total} posts{total > 0 && <> · busiest minute <b>{pad(zoom.h)}:{pad(peak)}</b> ({n(data.posts[peak])})</>}
        </span>
      </div>
      <div className="hours-plot fan-plot minute-plot" role="img" style={{ "--c": "var(--fan)" } as CSSProperties}
        aria-label={`Fan posts minute by minute, ${span(zoom.h)}${total > 0 ? `, busiest ${pad(zoom.h)}:${pad(peak)} with ${n(data.posts[peak])}` : ""}`}>
        {data.posts.map((v, m) => {
          const c = data.coverage[m], f = data.fans[m];
          const when = `${pad(zoom.h)}:${pad(m)}`;
          const tip = c < 0.001 ? `${when}\nno data (no capture read this minute)`
            : `${when}\n${avg ? `about ${avgFmt(v)} posts from about ${avgFmt(f)} fans` : `${plural(v, "post")} from ${plural(f, "fan")}`}` +
              (c < 0.999 ? `\n(${Math.round(c * 100)}% captured${avg ? " on average" : ", so a minimum"})` : "");
          return (
            <div key={m} className={`hours-col${m > 46 ? " tip-end" : m < 10 ? " tip-start" : ""}${pickable(m) ? " zoomable" : ""}`}
              tabIndex={0} data-tip={pickable(m) ? `${tip}
double-click to show these posts` : tip}
              onDoubleClick={() => pick(m)} onKeyDown={(e) => { if (e.key === "Enter") pick(m); }}>
              {c < 0.001 ? <div className="nodata-col" />
                : v > 0 && (
                  <div className={`hours-bar fan-posts${c < 0.999 ? " partial" : ""}`} style={{ height: `${(v / max) * 100}%` }}>
                    {f > 0 && <div className="fan-people" style={{ height: `${Math.min(1, f / v) * 100}%` }} />}
                  </div>
                )}
            </div>
          );
        })}
      </div>
      <ArtistStrip cells={artistCells} note={artistNote}
        label={`Artist activity minute by minute, ${span(zoom.h)}${artistTop ? `, most at ${artistTop}` : ", none seen"}`} />
      <div className="hours-axis" aria-hidden>
        {[0, 15, 30, 45].map((m) => <span key={m} style={{ left: `${(m / 60) * 100}%` }}>:{pad(m)}</span>)}
        <span style={{ right: 0 }}>:60</span>
      </div>
      <div className="muted small artist-sum">
        Artists {artistTop ? <>most often at <b>{artistTop}</b>{avg && " (all days)"}</> : avg ? "never seen at this hour yet" : "not seen this hour"}
      </div>
      <p className="muted small" style={{ marginBottom: 0 }}>
        Minute by minute, on this hour&apos;s own scale (tallest bar = {n(max)} posts).
        {onPick && t0 > 0 && " Double-click a minute to show its posts in the feed below."}
        {unseen > 0 && ` Striped: ${plural(unseen, "minute")} no capture read.`}
      </p>
    </>
  );
}

type Heat = "artist" | "posts";
const WEEKDAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"];
const sumBy = (by: Record<string, number>) => Object.values(by).reduce((a, b) => a + b, 0);
/** "Sat 21 Nov" for a YYYY-MM-DD date (calendar arithmetic only, no time zone). */
const dayName = (date: string, opts: Intl.DateTimeFormatOptions = { weekday: "short", day: "numeric", month: "short" }) =>
  new Intl.DateTimeFormat("en-GB", { timeZone: "UTC", ...opts }).format(new Date(`${date}T00:00:00Z`));

/** Every captured day as month calendars, each day shaded by how often artists came (member colours) or by fan
 * posts. Click a day to pick it. */
function DayCalendar({ days, heat, selected, today, onSelect }:
  { days: FanDay[]; heat: Heat; selected: string; today: string; onSelect: (date: string) => void }) {
  const byDate = new Map(days.map((d) => [d.date, d]));
  const value = (d: FanDay) => (heat === "artist" ? sumBy(d.artist) : d.posts);
  const max = Math.max(1, ...days.map(value));
  const months = [...new Set(days.map((d) => d.date.slice(0, 7)))];
  return (
    <div className="cal-months">
      {months.map((ym) => {
        const [y, m] = ym.split("-").map(Number);
        const lead = (new Date(Date.UTC(y, m - 1, 1)).getUTCDay() + 6) % 7; // Monday first
        const len = new Date(Date.UTC(y, m, 0)).getUTCDate();
        return (
          <div key={ym} className="cal-month">
            <div className="cal-title">{dayName(`${ym}-01`, { month: "long", year: "numeric" })}</div>
            <div className="cal-grid" role="grid" aria-label={dayName(`${ym}-01`, { month: "long", year: "numeric" })}>
              {WEEKDAYS.map((w) => <div key={w} className="cal-wd" aria-hidden>{w.slice(0, 2)}</div>)}
              {Array.from({ length: lead }, (_, i) => <div key={`b${i}`} />)}
              {Array.from({ length: len }, (_, i) => {
                const date = `${ym}-${pad(i + 1)}`, d = byDate.get(date);
                if (!d) return <div key={date} className="cal-day off" aria-hidden><span>{i + 1}</span></div>;
                const t = sumBy(d.artist), v = value(d), seen = Math.round(d.coverage * 100);
                const parts = Object.entries(d.artist).filter(([, n]) => n > 0).sort((a, b) => b[1] - a[1]);
                const empty = d.coverage === 0 && d.posts === 0;
                const col = (i + lead) % 7;
                const tip = `${dayName(date)}${date === today ? " (today)" : ""}\n` +
                  (empty ? "no data (the probe saw none of this day)" :
                    `${plural(d.posts, "post")} from ${plural(d.fans, "fan")}\nartists: ${t ? `${t} (${byText(d.artist)})` : "none seen"}\n` +
                    `${seen}% of the day${date === today ? " so far" : ""} captured${seen < 100 ? ", so counts are a minimum" : ""}\nclick to show this day`);
                return (
                  <button key={date} type="button" disabled={empty} data-tip={tip} aria-label={tip.replace(/\n/g, ", ")}
                    aria-pressed={date === selected} onClick={() => onSelect(date)}
                    className={`cal-day${empty ? " nodata" : ""}${seen < 50 && !empty ? " thin" : ""}${date === today ? " today" : ""}${col > 4 ? " tip-end" : col < 2 ? " tip-start" : ""}`}>
                    {v > 0 && (heat === "artist"
                      ? <span className="cal-heat" style={{ opacity: 0.3 + 0.7 * (v / max) }}>
                          {parts.map(([mb, n]) => <span key={mb} style={{ flexGrow: n, background: memberColor(mb) }} />)}
                        </span>
                      : <span className="cal-heat" style={{ background: "var(--fan)", opacity: 0.12 + 0.78 * (v / max) }} />)}
                    <span>{i + 1}</span>
                  </button>
                );
              })}
            </div>
          </div>
        );
      })}
    </div>
  );
}

/** One picked day, hour by hour: the same chart and artist strip as Past 24 hours, zoomable to minutes. */
function DayChart({ date, tz, max, onPick }: { date: string; tz: string; max: number; onPick?: (from: number) => void }) {
  const [data, setData] = useState<FanDayHours | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [zoom, setZoom] = useState<Zoom | null>(null);
  useEffect(() => {
    let stop = false;
    setData(null); setError(null); setZoom(null);
    fetch(`/api/fan-days?${new URLSearchParams({ tz, date })}`, { cache: "no-store" })
      .then(async (r) => { if (!r.ok) throw new Error(`HTTP ${r.status}`); return (await r.json()) as FanDayHours; })
      .then((d) => { if (!stop) setData(d); })
      .catch((e) => { if (!stop) setError(String(e.message ?? e)); });
    return () => { stop = true; };
  }, [date, tz]);

  if (error) return <div className="muted small">Couldn&apos;t load {dayName(date)} ({error}).</div>;
  if (!data) return <div className="muted small hours-sub">Loading {dayName(date)}…</div>;
  if (zoom) return <MinuteView zoom={zoom} tz={tz} onBack={() => setZoom(null)} onPick={onPick} />;

  const nowMs = Date.now();
  const hours = data.hours;
  const now = hours.findIndex((h) => { const a = Date.parse(h.at); return a <= nowMs && nowMs < a + HOUR; });
  const cols: HourCol[] = hours.map((h, i) => {
    const when = `${dayName(date)}, ${span(h.hour)}${i === now ? " (in progress)" : ""}`;
    if (Date.parse(h.at) > nowMs) return { v: 0, p: 0, kind: "none", tip: `${when}\nnot yet` };
    if (h.coverage === 0) return { v: 0, p: 0, kind: "none", tip: `${when}\nno data (the probe missed this hour)` };
    const who = `from ${plural(h.fans, "fan")}`;
    if (h.coverage < 0.999) return { v: h.posts, p: h.fans, kind: "partial", tip: `${when}\nat least ${plural(h.posts, "post")} ${who}\n(${Math.round(h.coverage * 100)}% of the hour captured)` };
    return { v: h.posts, p: h.fans, kind: "full", tip: `${when}\n${plural(h.posts, "post")} ${who}` };
  });
  const cells: ArtistCell[] = hours.map((h) => ({
    by: h.artist, when: `${dayName(date)}, ${span(h.hour)}`, href: activityRange(Date.parse(h.at), HOUR, tz),
  }));
  const top = topCells(cells, (i) => `${pad(hours[i].hour)}:00`);
  const peak = busiest(cols.map((c, i) => ({ v: c.v, h: i })).filter((x) => cols[x.h].kind !== "none"));
  const seen = Math.round(data.coverage * 100);
  return (
    <>
      <div className="muted small hours-sub">
        <b>{dayName(date, { weekday: "long", day: "numeric", month: "long" })}</b> · {plural(data.posts, "post")} from {plural(data.fans, "fan")}
        {peak && peak.v > 0 && <> · busiest <b>{span(hours[peak.h].hour)}</b> ({peak.v})</>}
        {seen < 100 && ` · ${seen}% captured`}
      </div>
      <FanHourPlot cols={cols} max={Math.max(max, ...hours.map((h) => h.posts))} now={now < 0 ? undefined : now}
        onZoom={(i) => setZoom({ chart: "day", h: hours[i].hour, at: Date.parse(hours[i].at) })}
        strip={<ArtistStrip cells={cells} note="likes and comments" label={`Artist activity on ${dayName(date)}${top ? `, most at ${top}` : ", none"}`} />}
        label={`Fan posts each hour on ${dayName(date)}, ${data.posts} in total`} />
      <div className="muted small artist-sum">Artists {top ? <>active at <b>{top}</b></> : "not seen this day"}</div>
    </>
  );
}

/** Average's "Pick a day": the calendar, then the picked day's chart. */
function DayPicker({ days, tz, max, onPick }: { days: FanDay[]; tz: string; max: number; onPick?: (from: number) => void }) {
  const today = days[days.length - 1]?.date ?? "";
  const [selected, setSelected] = useState(today);
  const [heat, setHeat] = useState<Heat>("artist");
  const pickable = days.filter((d) => d.coverage > 0 || d.posts > 0);
  const date = days.some((d) => d.date === selected) ? selected : today;
  const topDays = [...pickable].sort((a, b) => sumBy(b.artist) - sumBy(a.artist)).filter((d) => sumBy(d.artist) > 0).slice(0, 3);
  return (
    <>
      <div className="filters cal-heat-pick" role="group" aria-label="Shade days by">
        <span className="muted small">Shade days by</span>
        <button type="button" className="chip" aria-current={heat === "artist"} onClick={() => setHeat("artist")}>Artist likes &amp; comments</button>
        <button type="button" className="chip" aria-current={heat === "posts"} onClick={() => setHeat("posts")}>Fan posts</button>
      </div>
      <DayCalendar days={days} heat={heat} selected={date} today={today} onSelect={setSelected} />
      <div className="muted small artist-sum">
        {topDays.length ? <>Artists came most on <b>{topDays.map((d) => `${dayName(d.date)} (${sumBy(d.artist)})`).join(", ")}</b></> : "No artist activity seen yet"}
        {` · ${plural(pickable.length, "day")} captured`}
      </div>
      {date && <DayChart key={`${date}|${tz}`} date={date} tz={tz} max={max} onPick={onPick} />}
    </>
  );
}

const MIN_AVG_PCT = 80; // mirrors MinAvgCoverage in the API
type HourVal = { v: number; h: number };
const busiest = (xs: HourVal[]) => (xs.length ? xs.reduce((a, b) => (b.v > a.v ? b : a)) : null);
const quietest = (xs: HourVal[]) => (xs.length ? xs.reduce((a, b) => (b.v < a.v ? b : a)) : null);
const span = (h: number) => `${pad(h)}:00–${pad(h)}:59`;

/** Fan posts per hour of day, as two charts on one scale: the long-run average (fully captured hours only) and
 * the past 24 hours (the timeline's last 24 hours, each placed at its local hour of day). readOnly (the public
 * Main dashboard): minute-by-minute zoom still works, but nothing links to posts or Artist activity. */
export function FanHours({ fans, members, days, start, tz, tzLabel, readOnly = false }:
  { fans: FanActivity; members: MemberStats[]; days: FanDay[]; start: string; tz: string; tzLabel: string; readOnly?: boolean }) {
  // each chart zooms on its own, so both can show an hour at once
  const [avgZoom, setAvgZoom] = useState<Zoom | null>(null);
  const [pickDay, setPickDay] = useState(false); // Average: all days averaged, or the calendar and one day
  const [dayZoom, setDayZoom] = useState<Zoom | null>(null);
  useEffect(() => { setAvgZoom(null); setDayZoom(null); }, [tz]); // switching time zone goes back to the hours
  useEffect(() => { try { setPickDay(localStorage.getItem("fan-avg-mode") === "day"); } catch {} }, []);
  useEffect(() => { try { localStorage.setItem("fan-avg-mode", pickDay ? "day" : "avg"); } catch {} }, [pickDay]);
  const router = useRouter(), pathname = usePathname(), params = useSearchParams();
  const pickMinute = (from: number) => {
    const p = new URLSearchParams(params.toString());
    p.set("from", new Date(from).toISOString());
    p.set("to", new Date(from + 60e3).toISOString());
    router.push(`${pathname}?${p}`, { scroll: false });
  };
  // after picking a minute, bring the filtered feed into view
  const picked = params.get("from");
  useEffect(() => { if (picked) document.getElementById("feed-range")?.scrollIntoView({ behavior: "smooth", block: "start" }); }, [picked]);
  // past 24 h: the last timeline hour is the one in progress
  const n = fans.timeline.length, t0 = Date.parse(start);
  const day = Array.from({ length: 24 }, () => ({ v: 0, p: 0, c: 0, at: 0, i: -1 }));
  let now: number | undefined;
  for (let i = Math.max(0, n - 24); i < n; i++) {
    const at = t0 + i * HOUR;
    const h = Number(fmt(at, tz, { hour: "2-digit" }));
    day[h] = { v: fans.timeline[i], p: fans.people?.[i] ?? 0, c: fans.coverage[i], at, i };
    if (i === n - 1) now = h;
  }
  const max = Math.max(1, ...fans.hourAvg, ...day.map((d) => d.v)); // one scale, so the two charts compare

  const avgCols: HourCol[] = fans.hourAvg.map((v, h) => {
    const k = fans.hourN[h], p = fans.peopleAvg?.[h] ?? 0, part = fans.hourPartial?.[h] ?? 0;
    const of = part ? `(average of ${plural(k, "hour")}; ${part} only ${MIN_AVG_PCT}–99% captured, so slightly low)` : `(average of ${plural(k, "fully captured hour")})`;
    return k
      ? { v, p, kind: "full", tip: `${span(h)}\nabout ${avgFmt(v)} posts from about ${avgFmt(p)} fans an hour\n${of}` }
      : { v, p: 0, kind: "none", tip: `${span(h)}\nno hour captured at least 80% yet` };
  });
  const known = fans.hourAvg.map((v, h) => ({ v, h })).filter((x) => fans.hourN[x.h] > 0);
  const aPeak = busiest(known), aQuiet = quietest(known);
  const missing = 24 - known.length;

  const dayCols: HourCol[] = day.map((d, h) => {
    if (!d.at) return { v: 0, p: 0, kind: "none", tip: `${span(h)}\nno data` };
    const when = `${fmt(d.at, tz, { weekday: "short", day: "numeric", month: "short" })}, ${span(h)}${h === now ? " (in progress)" : ""}`;
    if (d.c === 0) return { v: 0, p: 0, kind: "none", tip: `${when}\nno data (the probe missed this hour)` };
    const who = `from ${plural(d.p, "fan")}`;
    if (d.c < 0.999) return { v: d.v, p: d.p, kind: "partial", tip: `${when}\nat least ${plural(d.v, "post")} ${who}\n(${Math.round(d.c * 100)}% of the hour captured)` };
    return { v: d.v, p: d.p, kind: "full", tip: `${when}\n${plural(d.v, "post")} ${who}` };
  });
  const dPeak = busiest(dayCols.map((c, h) => ({ v: c.v, h })).filter((x) => dayCols[x.h].kind !== "none"));
  const dTotal = day.reduce((a, d) => a + d.v, 0);
  const dGaps = dayCols.filter((c) => c.kind === "none").length;
  const dPartial = dayCols.filter((c) => c.kind === "partial").length;

  // when artists come: likes and comments per hour of day (all days), and in each of the past 24 hours
  const avgArtist: ArtistCell[] = Array.from({ length: 24 }, (_, h) => ({
    by: Object.fromEntries(members.map((m) => [m.member, m.hours[h] ?? 0])), when: span(h), href: activityAt(tz, h),
  }));
  const dayArtist: ArtistCell[] = day.map((d, h) => ({
    by: d.i < 0 ? {} : Object.fromEntries(members.map((m) => [m.member, m.timeline[d.i] ?? 0])),
    when: d.at ? `${fmt(d.at, tz, { weekday: "short", day: "numeric", month: "short" })}, ${span(h)}` : span(h),
    href: d.at ? activityRange(d.at, HOUR, tz) : undefined,
  }));
  const avgTop = topCells(avgArtist, (h) => `${pad(h)}:00`);
  const dayTop = topCells(dayArtist, (h) => `${pad(h)}:00`);

  const onPick = readOnly ? undefined : pickMinute;
  return (
    <LinksOn.Provider value={!readOnly}>
      <div className="fan-split">
        <div>
          <div className="fan-h-row">
            <h3 className="fan-h">{pickDay ? "One day" : "Average"}</h3>
            {days.length > 0 && (
              <div className="filters fan-mode" role="group" aria-label="Average or one day">
                <button type="button" className="chip" aria-current={!pickDay} onClick={() => setPickDay(false)}>Average of all days</button>
                <button type="button" className="chip" aria-current={pickDay} onClick={() => setPickDay(true)}>Pick a day</button>
              </div>
            )}
          </div>
          {pickDay ? <DayPicker days={days} tz={tz} max={max} onPick={onPick} /> : avgZoom ?<MinuteView zoom={avgZoom} tz={tz} onBack={() => setAvgZoom(null)} /> : (<>
          <div className="muted small hours-sub">
            {aPeak && aQuiet ? (
              <>Busiest <b>{span(aPeak.h)}</b> (about {avgFmt(aPeak.v)} posts, {avgFmt(fans.peopleAvg?.[aPeak.h] ?? 0)} fans) · quietest <b>{span(aQuiet.h)}</b> (about {avgFmt(aQuiet.v)})</>
            ) : "No complete hours captured yet"}
          </div>
          <FanHourPlot cols={avgCols} max={max} onZoom={(h) => setAvgZoom({ chart: "avg", h })}
            strip={<ArtistStrip cells={avgArtist} note="all days (likes and comments)"
              label={`Artist activity by hour of day${avgTop ? `, most at ${avgTop}` : ""}`} />}
            label={`Average fan posts per hour of day${aPeak ? `, busiest ${pad(aPeak.h)}:00 with about ${avgFmt(aPeak.v)}` : ""}`} />
          <div className="muted small artist-sum">Artists {avgTop ? <>most often at <b>{avgTop}</b> (all days)</> : "not seen yet"}</div>
          </>)}
          {pickDay ? (
            <p className="muted small" style={{ marginBottom: 0 }}>
              Every day the probe captured, shaded by how often artists liked or commented (member colours) or by fan posts. Click a day to see it hour by hour; hatched days were less than half
              captured. Double-click an hour for minute by minute{!readOnly && ", then a minute to list its posts below"}.
            </p>
          ) : <p className="muted small" style={{ marginBottom: 0 }}>
            Only finished hours the probe captured at least {MIN_AVG_PCT}% of count, so long gaps don&apos;t pull the
            averages down, while big bursts with a few unread minutes still count.
            {missing > 0 && ` Striped: ${plural(missing, "hour")} with no well-captured data yet.`}
          </p>}
        </div>
        <div>
          <h3 className="fan-h">Past 24 hours</h3>
          {dayZoom ? <MinuteView zoom={dayZoom} tz={tz} onBack={() => setDayZoom(null)} onPick={onPick} /> : (<>
          <div className="muted small hours-sub">
            <b>{plural(dTotal, "post")}</b>{dPeak && dPeak.v > 0 && <> · busiest <b>{span(dPeak.h)}</b> ({dPeak.v} posts from {plural(day[dPeak.h].p, "fan")})</>}
          </div>
          <FanHourPlot cols={dayCols} max={max} now={now} onZoom={(h) => setDayZoom({ chart: "day", h, at: day[h].at })}
            strip={<ArtistStrip cells={dayArtist} note="likes and comments"
              label={`Artist activity in the past 24 hours${dayTop ? `, most at ${dayTop}` : ", none"}`} />}
            label={`Fan posts in each of the past 24 hours, ${dTotal} in total${dPeak && dPeak.v > 0 ? `, most at ${pad(dPeak.h)}:00 with ${dPeak.v}` : ""}`} />
          <div className="muted small artist-sum">Artists {dayTop ? <>active at <b>{dayTop}</b></> : "not seen in the past 24 hours"}</div>
          </>)}
          <p className="muted small" style={{ marginBottom: 0 }}>
            Each hour&apos;s latest occurrence; columns after <b>now</b> are from yesterday.
            {dPartial > 0 && ` Faded: ${plural(dPartial, "hour")} only partly captured (counts are a minimum).`}
            {dGaps > 0 && ` Striped: ${plural(dGaps, "hour")} with no data.`}
          </p>
        </div>
      </div>
      <div className="legend" style={{ marginTop: 8 }}>
        <span className="tl-key"><span className="sw" style={{ background: "color-mix(in srgb, var(--fan) 35%, transparent)" }} />posts</span>
        <span className="tl-key"><span className="sw" style={{ background: "var(--fan)" }} />different fans posting</span>
        <span className="tl-key">
          <span className="sw artist-key" aria-hidden>{["LILY", "HAEWON", "SULLYOON", "BAE", "JIWOO", "KYUJIN"].map((m) => <span key={m} style={{ background: memberColor(m) }} />)}</span>
          strip: artists active (member colours, stronger = more often)
        </span>
      </div>
      <p className="muted small" style={{ marginBottom: 0 }}>
        {tzLabel} time. The strip under each chart shows when artists liked or commented (a like at its post&apos;s time); hover a cell for who{!readOnly && ", double-click it to list those likes and comments in a new tab"}.
        Both charts share one scale. A fan who posts 17 times in an hour counts once in the solid bar,
        so a tall light bar over a short solid one means a few people spamming. Double-click an hour (or press Enter on
        it) to see it minute by minute; each chart zooms on its own.
        {!readOnly && " In Past 24 hours, double-click a minute to list its posts in the feed below."}
      </p>
      <details className="section">
        <summary>Show as table</summary>
        <div className="table-wrap">
          <table>
            <thead><tr><th>Hour</th>{Array.from({ length: 24 }, (_, h) => <th key={h} className="num">{h}</th>)}</tr></thead>
            <tbody>
              <tr><td>Avg posts</td>{fans.hourAvg.map((v, h) => <td key={h} className="num">{fans.hourN[h] ? avgFmt(v) : "–"}</td>)}</tr>
              <tr><td>Avg fans</td>{(fans.peopleAvg ?? []).map((v, h) => <td key={h} className="num">{fans.hourN[h] ? avgFmt(v) : "–"}</td>)}</tr>
              <tr><td>Hours of data</td>{fans.hourN.map((k, h) => <td key={h} className="num">{k || ""}</td>)}</tr>
              <tr><td>Past 24 h posts</td>{dayCols.map((c, h) => <td key={h} className="num">{c.kind === "none" ? "–" : c.kind === "partial" ? `≥${c.v}` : c.v}</td>)}</tr>
              <tr><td>Past 24 h fans</td>{dayCols.map((c, h) => <td key={h} className="num">{c.kind === "none" ? "–" : c.kind === "partial" ? `≥${c.p}` : c.p}</td>)}</tr>
            </tbody>
          </table>
        </div>
      </details>
    </LinksOn.Provider>
  );
}

