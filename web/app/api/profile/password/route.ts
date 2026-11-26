// Change the signed-in person's password: {current, new}. Off on the home PC (its accounts are copies of the VPS's).
import { apiFetch } from "@/lib/api";
import { denyUnless, getSession, sameOrigin } from "@/lib/auth";
import { viaApi } from "@/lib/forward";

export const dynamic = "force-dynamic";

export async function POST(req: Request) {
  const denied = await denyUnless("user");
  if (denied) return denied;
  if (!sameOrigin(req)) return Response.json({ error: "forbidden" }, { status: 403 });
  const s = (await getSession())!;
  const body = await req.json().catch(() => null);
  if (typeof body?.current !== "string" || typeof body?.new !== "string") return Response.json({ error: "bad body" }, { status: 400 });
  return viaApi(() => apiFetch(`/api/profile/${s.name}/password`, {
    method: "POST", headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ current: body.current.slice(0, 1025), new: body.new.slice(0, 1025) }),
  }));
}
