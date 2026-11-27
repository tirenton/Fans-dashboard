// Public: is the probe's Trans-Am running? The whole site glows while it is (components/trans-am.tsx).
// Only { active, test, style, until } leaves the API (api/admin.go CurrentTransAm).
import { apiFetch } from "@/lib/api";

export const dynamic = "force-dynamic";

export async function GET() {
  try {
    const res = await apiFetch("/api/transam");
    if (!res.ok) return Response.json({ active: false }, { headers: { "Cache-Control": "no-store" } });
    const j = await res.json();
    return Response.json(
      {
        active: j?.active === true, test: j?.test === true, style: j?.style === "advance" ? "advance" : undefined,
        until: typeof j?.until === "string" ? j.until : undefined,
      },
      { headers: { "Cache-Control": "no-store" } },
    );
  } catch {
    return Response.json({ active: false }, { headers: { "Cache-Control": "no-store" } });
  }
}
