// Someone's profile picture, for signed-in people (the chat is only for them). ?v=<version> makes the URL change
// with every new picture, so browsers may keep each one for good.
import { apiFetch } from "@/lib/api";
import { denyUnless } from "@/lib/auth";

export const dynamic = "force-dynamic";

const NAME = /^[a-z0-9][a-z0-9_.-]{0,31}$/;

export async function GET(req: Request, { params }: { params: Promise<{ name: string }> }) {
  const denied = await denyUnless("user");
  if (denied) return denied;
  const { name } = await params;
  if (!NAME.test(name)) return new Response("bad name", { status: 400 });
  try {
    const res = await apiFetch(`/api/avatar/${name}`);
    if (!res.ok) return new Response(null, { status: res.status === 404 ? 404 : 502 });
    const versioned = /^\d{1,15}$/.test(new URL(req.url).searchParams.get("v") ?? "");
    return new Response(res.body, {
      headers: {
        "Content-Type": "image/jpeg",
        "X-Content-Type-Options": "nosniff",
        "Cache-Control": versioned ? "private, max-age=31536000, immutable" : "private, no-cache",
      },
    });
  } catch {
    return new Response(null, { status: 502 });
  }
}
