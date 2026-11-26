// Browser-facing proxy to the Go API's /api/fan-minutes (one hour of fan posts, minute by minute), so the
// zoomed fan chart can load without exposing the internal API address. Only these parameters pass through.
import { api, type FanMinutes } from "@/lib/api";

export const dynamic = "force-dynamic";

const ALLOWED = ["tz", "at", "hour"];

export async function GET(req: Request) {
  const src = new URL(req.url).searchParams;
  const p = new URLSearchParams();
  for (const k of ALLOWED) {
    const v = src.get(k);
    if (v) p.set(k, v);
  }
  try {
    const data = await api<FanMinutes>(`/api/fan-minutes?${p}`);
    return Response.json(data, { headers: { "Cache-Control": "no-store" } });
  } catch (e) {
    return Response.json({ error: String((e as Error)?.message ?? e) }, { status: 502 });
  }
}
