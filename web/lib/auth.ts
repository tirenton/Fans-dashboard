// Server-side role checks for pages and route handlers. proxy.ts checks the same rules first; these are the
// authoritative ones (the Next.js docs call the proxy an optimistic check only).
import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { allows, SESSION_COOKIE, verifySession, type Role, type Session } from "@/lib/session";

export async function getSession(): Promise<Session | null> {
  return verifySession((await cookies()).get(SESSION_COOKIE)?.value);
}

/** For pages: the session, or a redirect to the login page (coming back to `path` afterwards). */
export async function requireRole(need: Role, path: string): Promise<Session> {
  const s = await getSession();
  if (!allows(s?.role, need)) redirect(`/login?${new URLSearchParams({ next: path, ...(s ? { need } : {}) })}`);
  return s!;
}

/** For route handlers: null when allowed, else the 401/403 response to send. */
export async function denyUnless(need: Role): Promise<Response | null> {
  const s = await getSession();
  if (allows(s?.role, need)) return null;
  return Response.json({ error: s ? "forbidden" : "sign in first" }, { status: s ? 403 : 401 });
}

/** POSTs must come from this dashboard's own pages, not another site the browser has open. */
export function sameOrigin(req: Request) {
  const origin = req.headers.get("origin");
  try {
    return !!origin && new URL(origin).host === req.headers.get("host");
  } catch {
    return false;
  }
}
