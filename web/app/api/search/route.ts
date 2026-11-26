// Search popup: posts, fans and artist activity for one query, fetched from the Go API in parallel.
import { api, type ArtistEvent, type Feed, type FanMatch } from "@/lib/api";
import { denyUnless } from "@/lib/auth";

export const dynamic = "force-dynamic";

const SHOW = 6; // results per tab in the popup; "See all results" opens the full page
const EVENTS_MAX = 100; // the events endpoint has no total, so count up to this many

export async function GET(req: Request) {
  const denied = await denyUnless("admin");
  if (denied) return denied;
  const q = (new URL(req.url).searchParams.get("q") ?? "").trim().slice(0, 100);
  if (!q) return Response.json({ q, posts: { items: [], total: 0 }, fans: { items: [], total: 0 }, activity: { items: [], total: 0, more: false } });
  const enc = encodeURIComponent(q);
  try {
    const [feed, fans, events] = await Promise.all([
      api<Feed>(`/api/feed?q=${enc}&limit=${SHOW}`),
      api<{ fans: FanMatch[]; total: number }>(`/api/fans?q=${enc}&limit=${SHOW}`),
      api<ArtistEvent[]>(`/api/events?q=${enc}&limit=${EVENTS_MAX}`),
    ]);
    return Response.json(
      {
        q,
        posts: { items: feed.posts, total: feed.total },
        fans: { items: fans.fans, total: fans.total },
        activity: { items: events.slice(0, SHOW), total: events.length, more: events.length === EVENTS_MAX },
      },
      { headers: { "Cache-Control": "no-store" } },
    );
  } catch (e) {
    return Response.json({ error: String((e as Error)?.message ?? e) }, { status: 502 });
  }
}
