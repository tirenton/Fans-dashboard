// Browser-facing proxy to the Go API's /api/feed, so the live feed can poll without
// exposing the internal API address. Only these query parameters are passed through.
import { api, type Feed } from "@/lib/api";
import { denyUnless } from "@/lib/auth";

export const dynamic = "force-dynamic";

const ALLOWED = ["limit", "before", "beforeId", "q", "artist", "member", "from", "to", "hour", "minute", "tz"];

export async function GET(req: Request) {
  const denied = await denyUnless("admin");
  if (denied) return denied;
  const src = new URL(req.url).searchParams;
  const p = new URLSearchParams();
  for (const k of ALLOWED) {
    const v = src.get(k);
    if (v) p.set(k, v);
  }
  try {
    const feed = await api<Feed>(`/api/feed?${p}`);
    return Response.json(feed, { headers: { "Cache-Control": "no-store" } });
  } catch (e) {
    return Response.json({ error: String((e as Error)?.message ?? e) }, { status: 502 });
  }
}
