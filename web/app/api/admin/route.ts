// Browser-facing proxy for the Admin page. The Go API only writes files into requests/ (control.json, RESUME);
// the prototype's loop reads them before each pass. Never contacts FANS.
import { apiFetch } from "@/lib/api";
import { denyUnless, sameOrigin } from "@/lib/auth";
import { probeControls } from "@/lib/features";

export const dynamic = "force-dynamic";
const noStore = { "Cache-Control": "no-store" };

async function forward(res: Response) {
  if (res.ok) return Response.json(await res.json(), { headers: noStore });
  const text = (await res.text()).trim();
  return Response.json({ error: text || `API ${res.status}` }, { status: res.status === 409 ? 409 : res.status === 503 ? 503 : 502 });
}

export async function GET() {
  const denied = await denyUnless("admin");
  if (denied) return denied;
  if (!probeControls()) return Response.json({ error: "probe controls are on the home PC's dashboard" }, { status: 404 });
  try {
    return await forward(await apiFetch("/api/admin"));
  } catch (e) {
    return Response.json({ error: String((e as Error)?.message ?? e) }, { status: 502 });
  }
}

// body: { action: "control", paused?, saveImages?, showWindow? }, { action: "resume", stop }, { action: "start" },
// { action: "login" } or { action: "loop", command: "quit" | "restart" | "transam" | "transam-cancel" }
export async function POST(req: Request) {
  // only admins, and only from this dashboard's own pages (not some other site the browser has open)
  const denied = await denyUnless("admin");
  if (denied) return denied;
  if (!probeControls()) return Response.json({ error: "probe controls are on the home PC's dashboard" }, { status: 404 });
  if (!sameOrigin(req)) return Response.json({ error: "forbidden" }, { status: 403 });
  let body: Record<string, unknown>;
  try {
    body = await req.json();
  } catch {
    return Response.json({ error: "bad body" }, { status: 400 });
  }
  const { action, ...rest } = body;
  const paths: Record<string, string> = {
    control: "/api/admin/control", resume: "/api/admin/resume", start: "/api/admin/start", login: "/api/admin/login",
    loop: "/api/admin/loop",
  };
  const path = typeof action === "string" ? paths[action] : undefined;
  if (!path) return Response.json({ error: "unknown action" }, { status: 400 });
  try {
    return await forward(await apiFetch(path, {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(rest),
    }));
  } catch (e) {
    return Response.json({ error: String((e as Error)?.message ?? e) }, { status: 502 });
  }
}
