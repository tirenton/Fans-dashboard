// Uploads from the home PC's own API (api/push.go) to this Go API's sync routes, on the VPS only.
// No login here: the PC proves itself with SYNC_TOKEN instead. Without SYNC_TOKEN set, this route is a 404.
import { createHash, timingSafeEqual } from "node:crypto";
import { apiFetch } from "@/lib/api";

export const dynamic = "force-dynamic";

const PATHS = /^(state|users|admin|commands|commands\/ack|log|stop|missing-images|captures\/[^/]+|images\/[^/]+)$/;
const MAX_BYTES = 30 << 20;

function tokenOk(req: Request) {
  const token = process.env.SYNC_TOKEN ?? "";
  const digest = (s: string) => createHash("sha256").update(s).digest();
  return token.length >= 32 && timingSafeEqual(digest(req.headers.get("authorization") ?? ""), digest(`Bearer ${token}`));
}

async function forward(req: Request, ctx: { params: Promise<{ path: string[] }> }) {
  if ((process.env.SYNC_TOKEN ?? "").length < 32) return new Response("not found", { status: 404 });
  if (!tokenOk(req)) return new Response("unauthorized", { status: 401 });
  const { path } = await ctx.params;
  const sub = path.join("/");
  if (!PATHS.test(sub)) return new Response("not found", { status: 404 });
  if (Number(req.headers.get("content-length") ?? 0) > MAX_BYTES) return new Response("too large", { status: 413 });

  const q = new URL(req.url).searchParams;
  const offset = q.get("offset") ?? q.get("wait"); // ?offset= for the log, ?wait= for orders
  const qName = q.has("offset") ? "offset" : "wait";
  const headers: Record<string, string> = {};
  for (const h of ["content-encoding", "content-type"]) {
    const v = req.headers.get(h);
    if (v) headers[h] = v;
  }
  const hasBody = req.method === "PUT" || req.method === "POST";
  const res = await apiFetch(`/api/sync/${path.map(encodeURIComponent).join("/")}${offset !== null ? `?${qName}=${encodeURIComponent(offset)}` : ""}`, {
    method: req.method, headers,
    ...(hasBody ? { body: req.body, duplex: "half" } as RequestInit : {}),
  });
  const out: Record<string, string> = { "Cache-Control": "no-store" };
  for (const h of ["content-type", "x-log-size"]) {
    const v = res.headers.get(h);
    if (v) out[h] = v;
  }
  return new Response(res.status === 204 ? null : res.body, { status: res.status, headers: out });
}

export { forward as GET, forward as PUT, forward as POST, forward as DELETE };
