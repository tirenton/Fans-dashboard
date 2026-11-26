// Profile picture: PUT the image (the page sends a 256×256 JPEG it cropped; the API re-draws it anyway),
// DELETE to remove it. An admin can remove someone else's with DELETE ?name=<account>.
import { apiFetch } from "@/lib/api";
import { denyUnless, getSession, sameOrigin } from "@/lib/auth";
import { viaApi } from "@/lib/forward";

export const dynamic = "force-dynamic";

const MAX = 2 << 20;
const NAME = /^[a-z0-9][a-z0-9_.-]{0,31}$/;

export async function PUT(req: Request) {
  const denied = await denyUnless("user");
  if (denied) return denied;
  if (!sameOrigin(req)) return Response.json({ error: "forbidden" }, { status: 403 });
  const s = (await getSession())!;
  if (Number(req.headers.get("content-length") ?? 0) > MAX) return Response.json({ error: "image" }, { status: 400 });
  const data = await req.arrayBuffer();
  if (data.byteLength === 0 || data.byteLength > MAX) return Response.json({ error: "image" }, { status: 400 });
  return viaApi(() => apiFetch(`/api/profile/${s.name}/avatar`, {
    method: "PUT", headers: { "Content-Type": "application/octet-stream" }, body: data,
  }));
}

export async function DELETE(req: Request) {
  const denied = await denyUnless("user");
  if (denied) return denied;
  if (!sameOrigin(req)) return Response.json({ error: "forbidden" }, { status: 403 });
  const s = (await getSession())!;
  const other = new URL(req.url).searchParams.get("name");
  if (other && other !== s.name) {
    if (s.role !== "admin") return Response.json({ error: "forbidden" }, { status: 403 });
    if (!NAME.test(other)) return Response.json({ error: "bad name" }, { status: 400 });
  }
  return viaApi(() => apiFetch(`/api/profile/${other || s.name}/avatar`, { method: "DELETE" }));
}
