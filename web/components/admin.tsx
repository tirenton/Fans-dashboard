"use client";

import { useCallback, useEffect, useState } from "react";
import type { Admin, LoopCommand } from "@/lib/api";
import { fmtDuration } from "@/lib/format";

const POLL_MS = 5_000;

export function AdminPanel() {
  const [a, setA] = useState<Admin | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [now, setNow] = useState(() => Date.now());

  const read = useCallback(async () => {
    try {
      const r = await fetch("/api/admin", { cache: "no-store" });
      const j = await r.json();
      if (!r.ok) throw new Error(j.error ?? `HTTP ${r.status}`);
      setA(j);
    } catch (e) {
      setError(String((e as Error).message ?? e));
    }
  }, []);

  useEffect(() => {
    read();
    const id = setInterval(read, POLL_MS);
    const tick = setInterval(() => setNow(Date.now()), 1_000); // the countdown moves every second between reads
    return () => { clearInterval(id); clearInterval(tick); };
  }, [read]);

  const send = async (body: Record<string, unknown>) => {
    setBusy(true);
    setError(null);
    try {
      const r = await fetch("/api/admin", {
        method: "POST", cache: "no-store", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
      });
      const j = await r.json();
      if (!r.ok) throw new Error(j.error ?? `HTTP ${r.status}`);
      setA(j);
    } catch (e) {
      setError(String((e as Error).message ?? e));
      read();
    } finally {
      setBusy(false);
    }
  };

  if (!a) {
    return error
      ? <div className="card banner critical"><div><strong>Can&apos;t reach the API.</strong> <span className="muted">{error}</span></div></div>
      : <div className="card muted">Loading…</div>;
  }

  const loop = (command: LoopCommand) => send({ action: "loop", command });
  const c = a.control;
  const p = a.probe;
  const saveImages = c.saveImages ?? p?.saveImages ?? false;
  const showWindow = c.showWindow ?? p?.showWindow ?? false;

  return (
    <>
      {error && <div className="card banner critical"><div><strong>That didn&apos;t work.</strong> <span className="muted">{error}</span></div></div>}
      {a.remote && <RemoteBanner r={a.remote} now={now} />}
      {!a.enabled && !a.remote && (
        <div className="card banner warning">
          <div><strong>Controls are read-only.</strong>{" "}
            <span className="muted">The API has no writable requests folder (REQUESTS_DIR in docker-compose.yml).</span></div>
        </div>
      )}

      <Status a={a} now={now} busy={busy} onStart={() => send({ action: "start" })} onLoop={loop} />
      {a.stop && <StopCard a={a} busy={busy} onResume={() => send({ action: "resume", stop: a.stop })} />}
      <TransAmCard a={a} now={now} busy={busy} onLoop={loop} />
      <LoginCard a={a} now={now} busy={busy} onLogin={() => send({ action: "login" })} />
      <UploadCard a={a} now={now} />

      <section className="card section">
        <h2>Loop</h2>
        <Setting
          label="Passes"
          help={c.paused
            ? "Paused: no passes and no requests to FANS until you resume. The scheduled wait carries on from where it was."
            : "Running: a pass about every 30 minutes (every ~10 while an artist is active or a gap is open, every 45–60 at night 03–09 Bangkok, a few skipped at random), inside the account-safety limits."}
          options={[{ label: "Running", value: false }, { label: "Paused", value: true }]}
          value={c.paused} disabled={busy || !a.enabled}
          onChange={(v) => send({ action: "control", paused: v })}
        />
        <LoopButtons a={a} busy={busy} onLoop={loop} />
      </section>

      <section className="card section">
        <h2>Settings</h2>
        <p className="muted small" style={{ marginTop: 0 }}>Each applies from the probe&apos;s next pass.</p>
        <Setting
          label="Picture saving"
          help="Saves full-size images of posts with artist likes or comments, after a clean pass (its own limit: 20 an hour, 200 a day)."
          options={[{ label: "Off", value: false }, { label: "On", value: true }]}
          value={saveImages} disabled={busy || !a.enabled} note={c.saveImages === null ? "probe default" : undefined}
          onChange={(v) => send({ action: "control", saveImages: v })}
        />
        <Setting
          label="Chrome window"
          help="Hidden opens Chrome off-screen so it doesn't pop up while you work. Show it to log in to FANS or to watch a pass."
          options={[{ label: "Hidden", value: false }, { label: "On screen", value: true }]}
          value={showWindow} disabled={busy || !a.enabled} note={c.showWindow === null ? "probe default" : undefined}
          onChange={(v) => send({ action: "control", showWindow: v })}
        />
      </section>
    </>
  );
}

// Why the loop isn't running, from its last status.json and the launcher (the supervisor that restarts it).
function notRunning(a: Admin, now: number): string {
  const p = a.probe, l = a.launcher;
  const retry = l?.retryAt && a.launcherAlive ? Date.parse(l.retryAt) : NaN;
  if (Number.isFinite(retry)) {
    return `It stopped unexpectedly; the launcher starts it again ${retry > now ? `in ${clock(retry - now)}` : "any moment"}${l!.crashes > 1 ? ` (${l!.crashes} crashes in a row)` : ""}.`;
  }
  if (p?.state === "exited") {
    return p.reason === "quit" ? "It was stopped from this page."
      : p.reason === "restart" ? (a.launcherAlive ? "Restarting…" : "It was asked to restart, but the launcher isn't running to start it again.")
      : p.reason === "crashed" ? "It crashed." : "It was closed.";
  }
  return p ? `Last heard from ${fmtDuration(now - Date.parse(p.updatedAt))} ago.` : "";
}

function Status({ a, now, busy, onStart, onLoop }: {
  a: Admin; now: number; busy: boolean; onStart: () => void; onLoop: (c: LoopCommand) => void;
}) {
  const p = a.probe;
  let tone = "good", title = "", detail = "";
  if (!p || !a.alive) {
    tone = "warning";
    title = "The probe loop isn't running.";
    detail = notRunning(a, now);
    return (
      <div className={`card banner ${tone}`}>
        <div style={{ width: "100%" }}>
          <strong>{title}</strong> {detail && <span className="muted">{detail}</span>}
          {a.launcherAlive ? (
            <div className="filters" style={{ margin: "10px 0 0" }}>
              <button className="chip" aria-current="true" disabled={busy || !a.enabled || a.startPending} onClick={onStart}>
                {a.startPending ? "Starting…" : "Start probe"}
              </button>
              <span className="muted small">
                {a.startPending ? "The launcher on the PC starts it within a few seconds." : "Starts it in the background on the PC (no window)."}
              </span>
            </div>
          ) : (
            <div className="small muted" style={{ marginTop: 6 }}>
              The Start button needs the launcher running on the PC. In the prototype folder, run{" "}
              <code>node launcher.mjs</code> once (it keeps watching for Start). Or start the loop directly:{" "}
              <code>node probe.mjs loop</code>
            </div>
          )}
        </div>
      </div>
    );
  } else if (p.state === "login") {
    tone = "warning";
    title = "Waiting for you to log in to FANS.";
    detail = "A Chrome window is open on the PC. No passes until it closes.";
  } else if (a.stop) {
    tone = "critical";
    title = "Stopped by a safety halt.";
    detail = "No requests to FANS until the halt is cleared below.";
  } else if (p.state === "paused" || a.control.paused) {
    tone = "warning";
    title = "Paused.";
    detail = "Resume under Loop below.";
  } else if (p.state === "passing") {
    title = p.trigger === "pull" ? "Reading the feed now (pull)." : p.trigger === "trans-am" ? "Reading the feed now (Trans-Am)." : "Reading the feed now.";
  } else {
    title = p.transAm?.active ? "Trans-Am." : "Running.";
  }
  const restartPending = a.loopPending?.includes("restart");
  const passing = !a.stop && p.state === "passing";
  const waiting = !a.stop && !a.control.paused && p.state === "waiting" && !!p.nextPassAt;
  return (
    <div className={`card banner ${tone}`}>
      <div style={{ width: "100%" }}>
        <strong>{title}</strong> {detail && <span className="muted">{detail}</span>}
        {waiting && <Countdown from={a.lastPassAt} to={p.nextPassAt!} now={now} />}
        {passing && (
          <div className="countdown">
            <div className="progress busy" role="progressbar" aria-label="Pass in progress"><span /></div>
            <div className="countdown-row">
              <span>started {fmtDuration(Math.max(0, now - Date.parse(p.updatedAt)))} ago</span>
              <span>usually under a minute</span>
            </div>
          </div>
        )}
        {p.code?.changed && (
          <div className="filters" style={{ margin: "10px 0 6px" }}>
            <button className="chip" aria-current="true" disabled={busy || !a.enabled || !a.launcherAlive || restartPending} onClick={() => onLoop("restart")}>
              {restartPending ? "Restarting…" : "Restart to update"}
            </button>
            <span className="muted small">
              probe.mjs has changed since this loop started{a.launcherAlive ? "." : "; restarting needs the launcher running on the PC."}
            </span>
          </div>
        )}
        {p && <div className="small muted">pid {p.pid} · status updated {fmtDuration(Math.max(0, now - Date.parse(p.updatedAt)))} ago</div>}
      </div>
    </div>
  );
}

// Stop and Restart (requests/QUIT, requests/RESTART). The loop acts on them between passes, never during one.
function LoopButtons({ a, busy, onLoop }: { a: Admin; busy: boolean; onLoop: (c: LoopCommand) => void }) {
  const pending = a.loopPending ?? [];
  const off = busy || !a.enabled || !a.alive;
  const stop = () => {
    if (window.confirm("Stop the probe loop? It stays off (no passes) until you press Start.")) onLoop("quit");
  };
  return (
    <div style={{ display: "flex", flexWrap: "wrap", gap: 12, alignItems: "center", justifyContent: "space-between", padding: "10px 0" }}>
      <div style={{ flex: "1 1 260px" }}>
        <strong>Probe program</strong>
        <div className="muted small">
          {pending.includes("quit") ? "Stopping: right after the current pass, or within a few seconds."
            : pending.includes("restart") ? "Restarting: the loop closes and the launcher starts it again within seconds."
            : <>Restart closes the loop and starts it again with the current probe.mjs (after a code change). Stop closes it
              until you press Start. If it crashes, the launcher starts it again by itself.</>}
        </div>
      </div>
      <div className="filters" style={{ marginBottom: 0 }} role="group" aria-label="Probe program">
        <button className="chip" disabled={off || !a.launcherAlive || pending.includes("restart")} onClick={() => onLoop("restart")}
          title={a.launcherAlive ? undefined : "Needs the launcher running on the PC"}>Restart</button>
        <button className="chip" disabled={off || pending.includes("quit")} onClick={stop}>Stop</button>
      </div>
    </div>
  );
}

// Trans-Am: a pass about every 30 s for 5 min, for when a member is online right now (TRANSAM_* in probe.mjs).
// One page per pass, inside the hour/day caps; the probe refuses or ends it while the hour has TRANSAM_RESERVE
// requests or fewer left, so the passes after it still have budget.
const TRANSAM_RESERVE = 3, TRANSAM_MS = 5 * 60e3, TRANSAM_MAX_PASSES = 8; // ~30 s wait + ~10 s pass, for 5 min
function TransAmCard({ a, now, busy, onLoop }: { a: Admin; now: number; busy: boolean; onLoop: (c: LoopCommand) => void }) {
  const p = a.probe;
  if (!p || !a.alive || a.stop) return null;
  const t = p.transAm;
  const active = !!t?.active && !!t.until && Date.parse(t.until) > now - 60e3;
  const pending = a.loopPending?.find((c) => c.startsWith("transam"));
  const b = a.budget;
  const left = b ? Math.min(b.hourCap - b.lastHour, b.dayCap - b.lastDay) : 0;
  const passes = Math.max(0, Math.min(TRANSAM_MAX_PASSES, left - TRANSAM_RESERVE));
  const blocked = a.control.paused || p.state === "login";
  const until = active ? Date.parse(t!.until!) : NaN;
  const pct = active && t!.startedAt ? Math.min(100, Math.max(0, ((now - Date.parse(t!.startedAt)) / TRANSAM_MS) * 100)) : 0;
  return (
    <section className={`card section${active ? " banner good" : ""}`}>
      <div style={{ width: "100%" }}>
        <h2 style={{ marginTop: 0 }}>Trans-Am</h2>
        {active ? (
          <>
            <div className="countdown">
              <div className="countdown-row">
                <span><strong>TRANS-AM</strong> · {until > now ? <><strong>{clock(until - now)}</strong> left</> : "finishing"}</span>
                <span>{t!.passes} pass{t!.passes === 1 ? "" : "es"} so far</span>
              </div>
              <div className="progress" role="progressbar" aria-label="Trans-Am time used" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(pct)}>
                <span style={{ width: `${pct}%` }} />
              </div>
            </div>
            <div className="filters" style={{ margin: "10px 0 0" }}>
              <button className="chip" disabled={busy || !a.enabled || pending === "transam-cancel"} onClick={() => onLoop("transam-cancel")}>
                {pending === "transam-cancel" ? "Cancelling…" : "Cancel"}
              </button>
              <span className="muted small">Back to the normal schedule (about every 10 min after new artist activity).</span>
            </div>
          </>
        ) : (
          <>
            <p className="muted small" style={{ marginTop: 0 }}>
              A pass about every 30 seconds for 5 minutes, for when a member is online right now. One page each, inside
              the request limits ({b ? `${b.lastHour} of ${b.hourCap} used in the last hour` : "budget unknown"}). It
              stops early to keep {TRANSAM_RESERVE} requests for the passes after it.
            </p>
            <div className="filters" style={{ marginBottom: 0 }}>
              <button className="chip" aria-current="true" disabled={busy || !a.enabled || blocked || passes < 1 || pending === "transam"}
                onClick={() => onLoop("transam")}>
                {pending === "transam" ? "Starting…" : "Start Trans-Am"}
              </button>
              <span className="muted small">
                {pending === "transam" ? (p.state === "passing" ? "Right after the current pass." : "Within a few seconds.")
                  : blocked ? "Not while paused or logging in."
                  : passes < 1 ? "Not enough request budget left this hour; try again later."
                  : `Budget for about ${passes} pass${passes === 1 ? "" : "es"} now.`}
              </span>
            </div>
          </>
        )}
        {!active && t && t.endedAt && (
          <p className="small muted" style={{ margin: "8px 0 0" }}>
            Last Trans-Am: {t.passes} pass{t.passes === 1 ? "" : "es"}, ended at {hhmm(Date.parse(t.endedAt))} ({t.reason}).
          </p>
        )}
      </div>
    </section>
  );
}

const clock = (ms: number) => {
  const s = Math.max(0, Math.ceil(ms / 1000));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
};
const hhmm = (t: number) => new Date(t).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });

// Time left until the next scheduled pass, and a bar filling from the end of the last pass (the wait is 25-35 min,
// 8-12 while an artist is active, 45-60 at night: NORMAL_WAIT / FAST_WAIT / NIGHT_WAIT in probe.mjs; a random skip
// starts a new wait, logged as a skip).
const MAX_WAIT = 70 * 60e3; // longer than any wait the loop picks, so a longer one means the loop was down in between
function Countdown({ from, to, now }: { from?: string; to: string; now: number }) {
  const end = Date.parse(to);
  let start = from ? Date.parse(from) : NaN;
  // no bar from a pass logged after the wait began, or long before it (the loop was down in between)
  if (!(start < end) || end - start > MAX_WAIT) start = NaN;
  const left = end - now;
  const pct = Number.isFinite(start) ? Math.min(100, Math.max(0, ((now - start) / (end - start)) * 100)) : null;
  return (
    <div className="countdown">
      <div className="countdown-row">
        <span>{left > 0 ? <>Next pass in <strong>{clock(left)}</strong></> : <strong>Next pass any moment</strong>}</span>
        <span>at {hhmm(end)}</span>
      </div>
      {pct !== null && (
        <>
          <div className="progress" role="progressbar" aria-label="Wait until the next pass"
            aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(pct)}>
            <span style={{ width: `${pct}%` }} />
          </div>
          <div className="countdown-row">
            <span>last pass {hhmm(start)}</span>
            <span>{fmtDuration(end - start)} wait</span>
          </div>
        </>
      )}
    </div>
  );
}

const dayTime = (iso: string) =>
  new Date(iso).toLocaleString([], { weekday: "short", day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });

// FANS login: when it runs out (a fixed 3 days after logging in), and a button that has the loop open FANS on
// the PC's screen to log in. The loop handles it whenever it waits: halted, paused or between passes.
function LoginCard({ a, now, busy, onLogin }: { a: Admin; now: number; busy: boolean; onLogin: () => void }) {
  const exp = a.loginExpiresAt ? Date.parse(a.loginExpiresAt) : NaN;
  const left = exp - now;
  const expired = Number.isFinite(exp) && left <= 0;
  const soon = Number.isFinite(exp) && left > 0 && left < 12 * 3600e3;
  const loggedOut = !!a.stop && /\b401\b|logged out/i.test(a.stop);
  const open = a.probe?.state === "login" && a.alive;
  const tone = expired || loggedOut ? "critical" : soon ? "warning" : "";
  const last = a.login;
  return (
    <section className={`card section${tone ? ` banner ${tone}` : ""}`}>
      <div style={{ width: "100%" }}>
        <h2 style={{ marginTop: 0 }}>FANS login</h2>
        <p style={{ margin: "0 0 8px" }}>
          {!Number.isFinite(exp) ? <span className="muted">Expiry not known yet (it shows after the next pass).</span>
            : expired ? <><strong>Expired</strong> <span className="muted">{dayTime(a.loginExpiresAt!)} ({fmtDuration(left)} ago)</span></>
            : <>Valid until <strong>{dayTime(a.loginExpiresAt!)}</strong> <span className="muted">(in {fmtDuration(left)})</span></>}
        </p>
        <p className="muted small" style={{ marginTop: 0 }}>
          FANS logs the probe out a fixed 3 days after each login; refreshes don&apos;t extend it. Log in again before
          then, or after a 401 halt.
        </p>
        {open ? (
          <p className="small"><strong>Chrome is open on the PC.</strong>{" "}
            <span className="muted">Log in to FANS there (up to 15 min). It closes itself once you&apos;re in; closing it gives up.</span></p>
        ) : a.loginPending ? (
          <p className="small"><strong>Opening FANS on the PC…</strong>{" "}
            <span className="muted">{a.probe?.state === "passing" ? "Right after the current pass." : "Within a few seconds."}</span></p>
        ) : (
          <div className="filters" style={{ marginBottom: 0 }}>
            <button className="chip" aria-current={loggedOut || expired || soon ? "true" : undefined}
              disabled={busy || !a.enabled || !a.alive} onClick={onLogin}>Log in to FANS</button>
            <span className="muted small">
              {!a.alive ? "Needs the probe loop running (it opens Chrome)."
                : "Opens FANS in the probe's Chrome on the PC's screen, for you to log in."}
            </span>
          </div>
        )}
        {last && !open && last.endedAt && (
          <p className="small muted" style={{ marginBottom: 0 }}>
            Last login {last.state === "ok" ? "succeeded" : "ended without logging in"} at {dayTime(last.endedAt)}.
            {last.state === "ok" && a.stop && loggedOut && " Now clear the halt above."}
          </p>
        )}
      </div>
    </section>
  );
}

const ORDER_NAMES: Record<string, string> = {
  control: "settings change", start: "start", login: "log in to FANS", resume: "resume", pull: "pull now", loop: "stop / restart / Trans-Am",
};

/** On the VPS: these controls reach the probe through the home PC, which collects them within a second or two. */
function RemoteBanner({ r, now }: { r: NonNullable<Admin["remote"]>; now: number }) {
  const seen = r.seenAt ? `${fmtDuration(now - Date.parse(r.seenAt))} ago` : "never";
  const tone = !r.online ? "critical" : !r.accepts ? "warning" : "";
  return (
    <section className={`card section${tone ? ` banner ${tone}` : ""}`}>
      <div style={{ width: "100%" }}>
        <h2 style={{ marginTop: 0 }}>Your PC</h2>
        <p style={{ margin: "0 0 6px" }}>
          {!r.online ? <><strong>Offline</strong> <span className="muted">(last heard from {seen}). The probe runs on your PC; these controls work again once it&apos;s on.</span></>
            : !r.accepts ? <><strong>Online, but not taking orders from here.</strong> <span className="muted">Set VPS_REMOTE_CONTROL=1 in the PC&apos;s dashboard .env to allow it.</span></>
            : <><strong>Online</strong> <span className="muted">(checked in {seen}). Controls here go to the probe on your PC within a second or two.</span></>}
        </p>
        <p className="muted small" style={{ margin: 0 }}>
          {r.queued > 0 ? `${r.queued} order${r.queued === 1 ? "" : "s"} on the way to your PC. ` : ""}
          {r.last && <>Last order: {ORDER_NAMES[r.last.action] ?? r.last.action}, {fmtDuration(now - Date.parse(r.last.at))} ago{r.last.error ? <> — <span className="status warning">failed: {r.last.error}</span></> : ", done"}.</>}
        </p>
      </div>
    </section>
  );
}

/** Whether this PC's backend is keeping the VPS dashboard up to date (api/push.go). */
function UploadCard({ a, now }: { a: Admin; now: number }) {
  const u = a.upload;
  if (!u) {
    return (
      <section className="card section">
        <h2 style={{ marginTop: 0 }}>Upload to VPS</h2>
        <p className="muted small" style={{ margin: 0 }}>
          Not set up. Add VPS_URL and VPS_SYNC_TOKEN to the dashboard&apos;s .env on this PC and restart it to keep a
          VPS dashboard up to date.
        </p>
      </section>
    );
  }
  const ok = u.lastOk ? Date.parse(u.lastOk) : NaN;
  const stale = !Number.isFinite(ok) || now - ok > 15 * 60e3;
  const tone = u.error ? "critical" : stale ? "warning" : "";
  return (
    <section className={`card section${tone ? ` banner ${tone}` : ""}`}>
      <div style={{ width: "100%" }}>
        <h2 style={{ marginTop: 0 }}>Upload to VPS</h2>
        <p style={{ margin: "0 0 8px" }}>
          {u.error ? <><strong>Failing:</strong> <span className="muted">{u.error}</span></>
            : !Number.isFinite(ok) ? <span className="muted">Starting…</span>
            : <>Up to date with <strong>{u.host}</strong> <span className="muted">(checked {fmtDuration(now - ok)} ago)</span></>}
        </p>
        <p className="muted small" style={{ margin: 0 }}>
          {u.captures} captures on the VPS{u.pending > 0 && `, ${u.pending} still to send`}. Sent since this PC&apos;s
          dashboard started: {u.sent.captures} captures, {u.sent.images} images, {Math.round(u.sent.logBytes / 1024)} KB of log.
          It only sends when the probe wrote something new.
        </p>
        <p className="muted small" style={{ margin: "6px 0 0" }}>
          Orders from the VPS Admin page: {u.orders ? "taken" : "off (VPS_REMOTE_CONTROL in this PC's .env)"}
          {u.lastOrder && <> · last: {u.lastOrder.action} {fmtDuration(now - Date.parse(u.lastOrder.at))} ago{u.lastOrder.error ? ` (failed: ${u.lastOrder.error})` : ""}</>}
        </p>
        <p className="muted small" style={{ margin: "6px 0 0" }}>
          Accounts: {u.accountsError ? <span className="status warning">{u.accountsError}</span>
            : u.accountsAt ? <>{u.accounts} copied from the VPS {fmtDuration(now - Date.parse(u.accountsAt))} ago (every 5 min). Add or change accounts on the VPS.</>
            : "copying from the VPS…"}
        </p>
      </div>
    </section>
  );
}

function StopCard({ a, busy, onResume }: { a: Admin; busy: boolean; onResume: () => void }) {
  const [sure, setSure] = useState(false);
  useEffect(() => setSure(false), [a.stop]);
  return (
    <section className="card banner critical section">
      <div style={{ width: "100%" }}>
        <h2 style={{ marginTop: 0 }}>Safety halt</h2>
        <p style={{ margin: "0 0 8px" }}><code>{a.stop}</code></p>
        <p className="muted small">
          The probe wrote STOP itself (a 401/403/429, a logout or warning text on the page) and makes no requests to
          FANS until it is cleared. Fix the cause first: for a 401 or &quot;logged out&quot;, log in again with{" "}
          <strong>Log in to FANS</strong> below (the login lasts 3 days). For a 403 or 429, wait and check FANS by
          hand before resuming.
        </p>
        {a.resumePending ? (
          <p className="small"><strong>Resume requested.</strong> <span className="muted">The probe clears STOP within a few seconds{a.alive ? "" : " once its loop is running"}.</span></p>
        ) : (
          <div className="filters" style={{ marginBottom: 0 }}>
            <label className="small" style={{ display: "flex", gap: 6, alignItems: "center" }}>
              <input type="checkbox" checked={sure} onChange={(e) => setSure(e.target.checked)} disabled={!a.enabled} />
              I&apos;ve fixed the cause
            </label>
            <button className="chip" disabled={!sure || busy || !a.enabled} onClick={onResume}>Clear halt and resume</button>
          </div>
        )}
      </div>
    </section>
  );
}

function Setting<T extends boolean>({ label, help, options, value, onChange, disabled, note }: {
  label: string; help: string; options: { label: string; value: T }[]; value: T;
  onChange: (v: T) => void; disabled?: boolean; note?: string;
}) {
  return (
    <div style={{ display: "flex", flexWrap: "wrap", gap: 12, alignItems: "center", justifyContent: "space-between", padding: "10px 0" }}>
      <div style={{ flex: "1 1 260px" }}>
        <strong>{label}</strong>{note && <span className="muted small"> · {note}</span>}
        <div className="muted small">{help}</div>
      </div>
      <div className="filters" style={{ marginBottom: 0 }} role="group" aria-label={label}>
        {options.map((o) => (
          <button key={String(o.value)} className="chip" aria-current={value === o.value ? "true" : undefined}
            disabled={disabled} onClick={() => value !== o.value && onChange(o.value)}>
            {o.label}
          </button>
        ))}
      </div>
    </div>
  );
}
