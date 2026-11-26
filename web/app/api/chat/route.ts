// The Main dashboard's open chat. Any account may read and post; the name comes from the session, never the body.
// GET ?v=<version>&wait=25 holds on until there is something new (the page waits like this instead of polling).
import { apiFetch } from "@/lib/api";
import { denyUnless, getSession, sameOrigin } from "@/lib/auth";
import { forward } from "@/lib/forward";

export const dynamic = "force-dynamic";

export async function GET(req: Request) {
  const denied = await denyUnless("user");
  if (denied) return denied;
  const q = new URL(req.url).searchParams;
  const v = q.get("v") ?? "";
  const params = /^\d{1,20}$/.test(v) ? `?v=${v}&wait=25` : "";
  try {
    return await forward(await apiFetch(`/api/chat${params}`, { signal: req.signal }));
  } catch (e) {
    return Response.json({ error: String((e as Error)?.message ?? e) }, { status: 502 });
  }
}

export async function POST(req: Request) {
  const denied = await denyUnless("user");
  if (denied) return denied;
  if (!sameOrigin(req)) return Response.json({ error: "forbidden" }, { status: 403 });
  const s = (await getSession())!;
  const body = await req.json().catch(() => null);
  if (typeof body?.text !== "string") return Response.json({ error: "bad body" }, { status: 400 });
  try {
    return await forward(await apiFetch("/api/chat", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: s.name, admin: s.role === "admin", text: body.text.slice(0, 4000) }),
    }));
  } catch (e) {
    return Response.json({ error: String((e as Error)?.message ?? e) }, { status: 502 });
  }
}
