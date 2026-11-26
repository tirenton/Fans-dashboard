// The signed-in person's own profile: GET it, PUT {display} to change the name shown in the chat.
import { apiFetch } from "@/lib/api";
import { denyUnless, getSession, sameOrigin } from "@/lib/auth";
import { viaApi } from "@/lib/forward";

export const dynamic = "force-dynamic";

export async function GET() {
  const denied = await denyUnless("user");
  if (denied) return denied;
  const s = (await getSession())!;
  return viaApi(() => apiFetch(`/api/profile/${s.name}`));
}

export async function PUT(req: Request) {
  const denied = await denyUnless("user");
  if (denied) return denied;
  if (!sameOrigin(req)) return Response.json({ error: "forbidden" }, { status: 403 });
  const s = (await getSession())!;
  const body = await req.json().catch(() => null);
  if (typeof body?.display !== "string") return Response.json({ error: "bad body" }, { status: 400 });
  return viaApi(() => apiFetch(`/api/profile/${s.name}/display`, {
    method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ display: body.display.slice(0, 200) }),
  }));
}
