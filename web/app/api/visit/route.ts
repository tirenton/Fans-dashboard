// One page view, sent by the page's own script (components/visit-beacon.tsx). Admins aren't counted. The API turns
// the address and browser into a code that only lasts for today (api/visits.go) and never stores the address.
import { apiFetch } from "@/lib/api";
import { getSession, sameOrigin } from "@/lib/auth";
import { clientIp } from "@/lib/accounts";

export const dynamic = "force-dynamic";

export async function POST(req: Request) {
  if (!sameOrigin(req)) return new Response(null, { status: 403 });
  const s = await getSession();
  if (s?.role === "admin") return new Response(null, { status: 204 });
  const body = await req.json().catch(() => null);
  const path = typeof body?.path === "string" ? body.path.slice(0, 80) : "";
  const ref = typeof body?.ref === "string" ? body.ref.slice(0, 80) : "";
  // the day of this browser's last visit, as the API told it: lets the API count unique visitors over 7 and 30 days
  const last = typeof body?.last === "string" && /^\d{4}-\d{2}-\d{2}$/.test(body.last) ? body.last : "";
  let day = "";
  try {
    const res = await apiFetch("/api/visits/hit", {
      method: "POST", headers: { "Content-Type": "application/json" },
      // account: the signed-in account (the site knows it anyway), so unique accounts are exact across browsers
      body: JSON.stringify({ ip: clientIp(req), ua: (req.headers.get("user-agent") ?? "").slice(0, 400), path, ref, last, member: !!s, account: s?.name ?? "" }),
    });
    const d = (await res.json().catch(() => null))?.day;
    if (typeof d === "string") day = d;
  } catch {}
  return Response.json({ day }, { headers: { "Cache-Control": "no-store" } });
}
