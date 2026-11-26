// Probe health, on the Admin page (it used to be its own Health page): is the probe running, is it inside the
// account-safety caps, and what it did last. Server component; reads /api/health (log.jsonl and STOP).
import { api, type Health } from "@/lib/api";
import { Tile } from "@/components/ui";
import { Freshness, LocalTime, Relative } from "@/components/client";

const RECENT = 10;

export async function HealthSection() {
  let h: Health;
  try {
    h = await api<Health>("/api/health");
  } catch {
    return (
      <section className="section">
        <h2>Probe health</h2>
        <p className="status warning">▲ Couldn&apos;t read the probe&apos;s log from the API.</p>
      </section>
    );
  }
  const peak = Math.max(h.hourCap, ...h.hourly.map((b) => b.requests));

  return (
    <section className="section" aria-labelledby="health-title">
      <h2 id="health-title">Probe health</h2>
      {h.logMissing ? (
        <p className="muted small">No probe log yet (log.jsonl wasn&apos;t found in the data folder).</p>
      ) : (
        <>
          <div className={`card banner ${h.stop ? "critical" : "good"}`}>
            <Freshness lastPassAt={h.lastPassAt} stopped={!!h.stop} />
            <div>
              {h.stop ? (
                <><strong>The probe stopped itself.</strong> <span className="muted">{h.stop}</span></>
              ) : (
                <>Last check <Relative iso={h.lastPassAt} />, last successful capture <Relative iso={h.lastCaptureOkAt} />.</>
              )}
            </div>
          </div>

          <div className="grid-tiles">
            <Tile label="FANS requests · last hour" value={<>{h.requestsLastHour}<span className="muted small"> / {h.hourCap}</span></>}
              sub={h.requestsLastHour > h.hourCap ? <span className="status critical">■ Over the hourly cap</span> : "within the cap"} />
            <Tile label="FANS requests · last 24 h" value={<>{h.requestsLast24h}<span className="muted small"> / {h.dayCap}</span></>}
              sub={h.requestsLast24h > h.dayCap ? <span className="status critical">■ Over the daily cap</span> : "within the cap"} />
            <Tile label="Checks" value={h.passes} sub={`${h.skips} skipped (random or budget) · ${h.warns} warnings`} />
          </div>

          <section className="card">
            <h2>FANS requests per hour · last 48 h</h2>
            <div className="cols-wrap">
              <div className="cols" role="img" aria-label="FANS requests per hour, last 48 hours">
                {h.hourly.map((b) => (
                  <div key={b.hour} className="col-hit" tabIndex={0}
                    data-tip={`${new Date(b.hour).toLocaleString([], { weekday: "short", hour: "2-digit", minute: "2-digit" })}\n${b.requests} requests · ${b.passes} checks`}>
                    <div className={`col${b.requests > h.hourCap ? " over" : ""}`} style={{ height: b.requests ? `${(b.requests / peak) * 100}%` : 0 }} />
                  </div>
                ))}
                <div className="capline" style={{ bottom: `${(h.hourCap / peak) * 100}%` }}><span>cap {h.hourCap}/h</span></div>
              </div>
            </div>
            <div className="axis"><span>48 h ago</span><span>24 h ago</span><span>now</span></div>
          </section>

          <details className="section">
            <summary>Last {RECENT} log entries</summary>
            <div className="table-wrap">
              <table>
                <thead><tr><th>Time</th><th>What</th><th className="num">Requests</th><th className="num">Posts</th><th>Note</th></tr></thead>
                <tbody>
                  {h.recent.slice(0, RECENT).map((r, i) => (
                    <tr key={r.at + i}>
                      <td><LocalTime iso={r.at} /></td>
                      <td>{r.kind === "pass" ? `check${r.type ? ` (${r.type})` : ""}` : r.kind}</td>
                      <td className="num">{r.kind === "pass" ? r.requests : ""}</td>
                      <td className="num">{r.kind === "pass" ? r.posts : ""}</td>
                      <td className="muted">{r.note || ""}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </details>
        </>
      )}
    </section>
  );
}
