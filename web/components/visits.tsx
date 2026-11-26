"use client";

// Admin page: the visitor counter (api/visits.go). Counts come from the page's own script, so bots that don't run
// scripts and admins are left out. A visitor is counted once per day; nobody's address is ever stored.
import { useCallback, useEffect, useState } from "react";
import { Tile } from "@/components/ui";

type Count = { day: string; visitors: number; members: number; views: number; new: number };
type Top = { key: string; count: number };
// unique browsers over a window; members: those signed in at their first view of a day; accounts: distinct accounts
type Unique = { days: number; unique: number; new: number; visitors: number; members?: number; accounts?: number };
// all-time running totals (api/visits.go VisitTotals), kept after the daily rows are dropped
type AllTime = {
  since: string; visitors: number; views: number; unique: number; uniqueSince?: string;
  uniqueMembers?: number; accounts?: number; accountsSince?: string;
};
type Summary = { tz: string; online: number; onlineMembers?: number; days: Count[]; pages: Top[]; refs: Top[]; unique?: Unique[]; allTime?: AllTime };

const n = (v: number) => v.toLocaleString();
const dayName = (day: string, opts: Intl.DateTimeFormatOptions) =>
  new Date(`${day}T12:00:00Z`).toLocaleDateString(undefined, { timeZone: "UTC", ...opts });

const PAGE_NAMES: Record<string, string> = {
  "/": "Main dashboard", "/login": "Log in", "/signup": "Sign up", "/profile": "Profile", "/forgot": "Forgot password",
};

function TopList({ title, items, empty, label }: { title: string; items: Top[]; empty: string; label: (k: string) => string }) {
  const peak = Math.max(1, ...items.map((i) => i.count));
  return (
    <section className="card">
      <h2>{title}</h2>
      {items.length === 0 ? (
        <p className="muted small" style={{ margin: 0 }}>{empty}</p>
      ) : (
        <div className="vt-list">
          {items.map((i) => (
            <div key={i.key} className="vt-row">
              <span className="vt-key" title={i.key}>{label(i.key)}</span>
              <span className="vt-track"><span className="vt-bar" style={{ width: `${(i.count / peak) * 100}%` }} /></span>
              <span className="vt-n">{n(i.count)}</span>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

export function VisitsPanel() {
  const [s, setS] = useState<Summary | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const res = await fetch("/api/visits", { cache: "no-store" });
      if (!res.ok) throw new Error(`Couldn't load the visitor counts (${res.status}).`);
      setS(await res.json());
      setError(null);
    } catch (e) {
      setError(String((e as Error).message));
    }
  }, []);
  useEffect(() => {
    load();
    const id = setInterval(load, 60_000);
    return () => clearInterval(id);
  }, [load]);

  if (!s) {
    return (
      <section className="section">
        <h2>Visitors</h2>
        {error ? <p className="status critical" role="alert">{error}</p> : <p className="muted small">Loading…</p>}
      </section>
    );
  }
  const today = s.days[s.days.length - 1];
  const sum = (k: keyof Omit<Count, "day">, days: number) => s.days.slice(-days).reduce((a, d) => a + (d[k] ?? 0), 0);
  const peak = Math.max(1, ...s.days.map((d) => d.visitors));
  const uniq = (days: number) => s.unique?.find((u) => u.days === days);
  // "2 signed in · 4 not signed in" (signed in at their first view of a day)
  const split = (total: number, members?: number) =>
    members === undefined ? "" : `${n(members)} signed in · ${n(Math.max(0, total - members))} not signed in`;
  const longDay = (day: string) => dayName(day, { day: "numeric", month: "short", year: "numeric" });
  const uniqueTile = (days: number) => {
    const u = uniq(days);
    return (
      <Tile
        label={`Unique visitors · ${days} days`} value={u ? n(u.unique) : "–"}
        sub={u ? [split(u.unique, u.members), `${n(u.new)} new`].filter(Boolean).join(" · ") : "each person counted once"}
      />
    );
  };
  const acc7 = uniq(7)?.accounts, acc30 = uniq(30)?.accounts, accAll = s.allTime?.accounts;

  return (
    <section className="section visits" aria-labelledby="visits-title">
      <div className="chat-head">
        <h2 id="visits-title">Visitors</h2>
        <button type="button" className="chip" onClick={load}>Refresh</button>
      </div>
      {error && <p className="status critical" role="alert">{error}</p>}
      <div className="grid-tiles">
        <Tile label="Online now" value={n(s.online)}
          sub={s.onlineMembers === undefined ? "viewed a page in the last 5 min"
            : `${n(s.onlineMembers)} signed in · ${n(s.online - s.onlineMembers)} not signed in (last 5 min)`} />
        <Tile label="Visitors today" value={n(today.visitors)} sub={`${n(today.members)} signed in`} />
        <Tile label="Page views today" value={n(today.views)} />
        <Tile label="Last 7 days" value={n(sum("visitors", 7))} sub={`${n(sum("views", 7))} page views`} />
        <Tile label="Last 30 days" value={n(sum("visitors", 30))} sub={`${n(sum("views", 30))} page views`} />
        {uniqueTile(7)}
        {uniqueTile(30)}
        {s.allTime && (
          <>
            <Tile label="All time" value={n(s.allTime.visitors)}
              sub={`${n(s.allTime.views)} page views${s.allTime.since ? ` · since ${longDay(s.allTime.since)}` : ""}`} />
            <Tile label="Unique visitors · all time" value={n(s.allTime.unique)}
              sub={[split(s.allTime.unique, s.allTime.uniqueMembers),
                s.allTime.uniqueSince ? `since ${longDay(s.allTime.uniqueSince)}` : ""].filter(Boolean).join(" · ") || "each person counted once"} />
          </>
        )}
        {acc30 !== undefined && (
          <Tile label="Signed-in accounts · 30 days" value={n(acc30)}
            sub={`7 days: ${n(acc7 ?? 0)} · all time: ${n(accAll ?? 0)}${s.allTime?.accountsSince ? ` (since ${longDay(s.allTime.accountsSince)})` : ""} · each account once, any browser`} />
        )}
      </div>

      <section className="card">
        <h2>Visitors per day · last 30 days</h2>
        <div className="cols-wrap">
          <div className="cols vt-cols" role="img" aria-label={`Visitors per day for the last 30 days; today ${today.visitors}, busiest day ${peak}`}>
            {s.days.map((d) => (
              <div
                key={d.day} className="col-hit" tabIndex={0}
                data-tip={`${dayName(d.day, { weekday: "short", day: "numeric", month: "short" })}\n${n(d.visitors)} visitor${d.visitors === 1 ? "" : "s"} · ${n(d.members)} signed in\n${n(d.views)} page view${d.views === 1 ? "" : "s"}`}
              >
                <div className="col" style={{ height: d.visitors ? `${Math.max(2, (d.visitors / peak) * 100)}%` : 0 }} />
              </div>
            ))}
            <div className="capline" style={{ bottom: "100%" }}><span>{n(peak)}</span></div>
          </div>
        </div>
        <div className="axis">
          <span>{dayName(s.days[0].day, { day: "numeric", month: "short" })}</span>
          <span>{dayName(s.days[15].day, { day: "numeric", month: "short" })}</span>
          <span>today</span>
        </div>
        <p className="muted small" style={{ marginBottom: 0 }}>
          A visitor is counted once a day (days in {s.tz}). Admins and bots aren&apos;t counted. No addresses are stored.
          Unique visitors count each browser once over the whole period; one that clears its storage, or a private
          window, counts as new again.
        </p>
      </section>

      <div className="vt-grid">
        <TopList title="Top pages · 7 days" items={s.pages} empty="No page views yet." label={(k) => PAGE_NAMES[k] ?? k} />
        <TopList title="Where visitors came from · 7 days" items={s.refs} empty="Nobody arrived from another site yet." label={(k) => k} />
      </div>
    </section>
  );
}
