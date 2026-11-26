// Serves a picture the prototype saved to images/, via the Go API. Never fetches from FANS.
import { apiFetch } from "@/lib/api";
import { denyUnless } from "@/lib/auth";

export const dynamic = "force-dynamic";

export async function GET(_req: Request, ctx: { params: Promise<{ name: string }> }) {
  const denied = await denyUnless("admin");
  if (denied) return denied;
  const { name } = await ctx.params;
  if (!/^[A-Za-z0-9_-][A-Za-z0-9._-]*$/.test(name)) return new Response("not found", { status: 404 });
  const res = await apiFetch(`/api/images/${encodeURIComponent(name)}`);
  if (!res.ok) return new Response("not found", { status: res.status === 404 ? 404 : 502 });
  return new Response(res.body, {
    headers: {
      "Content-Type": res.headers.get("content-type") ?? "application/octet-stream",
      "Cache-Control": "private, max-age=31536000, immutable",
      "X-Content-Type-Options": "nosniff",
    },
  });
}
