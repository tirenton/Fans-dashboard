// Browser-facing proxy for "Pull now". The Go API only writes requests/PULL; the prototype's loop
// picks it up and runs its next pass early, still inside its request budget. Never contacts FANS.
import { api, apiFetch, type PullStatus } from "@/lib/api";
import { denyUnless, sameOrigin } from "@/lib/auth";
import { probeControls } from "@/lib/features";

export const dynamic = "force-dynamic";

export async function GET() {
  const denied = await denyUnless("admin");
  if (denied) return denied;
  try {
    return Response.json(await api<PullStatus>("/api/pull"), { headers: { "Cache-Control": "no-store" } });
  } catch (e) {
    return Response.json({ error: String((e as Error)?.message ?? e) }, { status: 502 });
  }
}

export async function POST(req: Request) {
  // only admins, and only from this dashboard's own pages (not some other site the browser has open)
  const denied = await denyUnless("admin");
  if (denied) return denied;
  if (!probeControls()) return Response.json({ error: "pull runs from the home PC's dashboard" }, { status: 404 });
  if (!sameOrigin(req)) return Response.json({ error: "forbidden" }, { status: 403 });
  const res = await apiFetch("/api/pull", { method: "POST" });
  if (!res.ok) return Response.json({ error: `API ${res.status}` }, { status: res.status === 503 ? 503 : 502 });
  return Response.json(await res.json(), { headers: { "Cache-Control": "no-store" } });
}
