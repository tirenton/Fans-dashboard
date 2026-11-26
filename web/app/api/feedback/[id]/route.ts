// Admins mark a piece of feedback done, which removes it.
import { apiFetch } from "@/lib/api";
import { denyUnless, sameOrigin } from "@/lib/auth";
import { forward } from "@/lib/forward";

export const dynamic = "force-dynamic";

export async function DELETE(req: Request, { params }: { params: Promise<{ id: string }> }) {
  const denied = await denyUnless("admin");
  if (denied) return denied;
  if (!sameOrigin(req)) return Response.json({ error: "forbidden" }, { status: 403 });
  const { id } = await params;
  if (!/^\d{1,18}$/.test(id)) return Response.json({ error: "bad id" }, { status: 400 });
  try {
    return await forward(await apiFetch(`/api/feedback/${id}`, { method: "DELETE" }));
  } catch (e) {
    return Response.json({ error: String((e as Error)?.message ?? e) }, { status: 502 });
  }
}
