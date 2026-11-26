// Totals on the Admin page (they used to head the Artist activity page): artist likes and comments, fan posts
// tracked, and how many captures the API has read. Server component; reads /api/stats.
import { api, type Stats } from "@/lib/api";
import { Tile } from "@/components/ui";
import { Relative } from "@/components/client";

export async function AdminStats() {
  const stats = await api<Stats>("/api/stats").catch(() => null);
  if (!stats) return null; // the Probe health section below says when the API can't be read
  const likes = stats.members.reduce((n, m) => n + m.likes, 0);
  const comments = stats.members.reduce((n, m) => n + m.comments, 0);
  const late = stats.members.reduce((n, m) => n + (m.late ?? 0), 0);
  return (
    <section className="section" aria-labelledby="totals-title">
      <h2 id="totals-title">Totals</h2>
      <div className="grid-tiles">
        <Tile label="Artist likes" value={likes} sub={`${comments} artist comments${late ? ` · ${late} late likes` : ""}`} />
        <Tile label="Fan posts tracked" value={stats.postsTracked} sub={`${stats.postsWithArtist} with artist activity`} />
        <Tile label="Captures read" value={stats.captures} sub={<>last <Relative iso={stats.lastCaptureAt} /></>} />
      </div>
    </section>
  );
}
