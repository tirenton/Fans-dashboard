// Feedback from the bottom of the Main dashboard. Any account may send it (the name comes from the session);
// only admins may read it, on the Admin page.
import { apiFetch } from "@/lib/api";
import { denyUnless, getSession, sameOrigin } from "@/lib/auth";
import { forward } from "@/lib/forward";

export const dynamic = "force-dynamic";

export async function GET() {
  const denied = await denyUnless("admin");
  if (denied) return denied;
  try {
    return await forward(await apiFetch("/api/feedback"));
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
    return await forward(await apiFetch("/api/feedback", {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ name: s.name, text: body.text.slice(0, 8000) }),
    }));
  } catch (e) {
    return Response.json({ error: String((e as Error)?.message ?? e) }, { status: 502 });
  }
}
