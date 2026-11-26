// Browser-facing proxy to the Go API's /api/fan-days (the calendar of captured days, or one day hour by hour), so the
// Fan feed calendar can load without exposing the internal API address. Only these parameters pass through.
import { api, type FanDay, type FanDayHours } from "@/lib/api";

export const dynamic = "force-dynamic";

const ALLOWED = ["tz", "date"];

export async function GET(req: Request) {
  const src = new URL(req.url).searchParams;
  const p = new URLSearchParams();
  for (const k of ALLOWED) {
    const v = src.get(k);
    if (v) p.set(k, v);
  }
  try {
    const data = await api<FanDay[] | FanDayHours>(`/api/fan-days?${p}`);
    return Response.json(data, { headers: { "Cache-Control": "no-store" } });
  } catch (e) {
    return Response.json({ error: String((e as Error)?.message ?? e) }, { status: 502 });
  }
}
