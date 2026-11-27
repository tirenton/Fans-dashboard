// Who may open what. Public: the Main dashboard (/), the login page, and the two read-only chart endpoints it
// uses. Any account: the Main dashboard's chat, sending feedback, and their profile. Everything else is for admins.
// Pages and route handlers check again with lib/auth.ts; this proxy only turns people away early.
import { NextResponse, type NextRequest } from "next/server";
import { allows, SESSION_COOKIE, verifySession, type Role } from "@/lib/session";

const PUBLIC = new Set([
  "/", "/login", "/api/auth/login", "/api/auth/logout", "/api/fan-minutes", "/api/fan-days",
  "/api/visit", // the visitor counter's page-view report (admins read the totals on /api/visits)
  "/api/transam", // whether the probe's Trans-Am is running, for the site-wide glow (nothing else)
  // signing up and resetting a password by email (lib/accounts.ts)
  "/signup", "/confirm", "/forgot", "/reset", "/api/auth/signup", "/api/auth/confirm", "/api/auth/forgot", "/api/auth/reset",
]);

function needs(path: string, method: string): Role | null {
  if (PUBLIC.has(path)) return null;
  if (path.startsWith("/api/sync/")) return null; // the home PC's uploader: checked against SYNC_TOKEN in its route
  if (path === "/api/chat" || (path === "/api/feedback" && method === "POST")) return "user";
  // your own profile and picture, and everyone's pictures in the chat (removing someone else's: checked in the route)
  if (path === "/profile" || path === "/api/profile" || path.startsWith("/api/profile/") || path.startsWith("/api/avatar/")) return "user";
  return "admin"; // deleting chat messages and reading feedback included
}

export async function proxy(req: NextRequest) {
  const path = req.nextUrl.pathname;
  const need = needs(path, req.method);
  if (!need) return NextResponse.next();
  const s = await verifySession(req.cookies.get(SESSION_COOKIE)?.value);
  if (allows(s?.role, need)) return NextResponse.next();
  if (path.startsWith("/api/") || path.startsWith("/img/")) {
    return NextResponse.json({ error: s ? "forbidden" : "sign in first" }, { status: s ? 403 : 401 });
  }
  const url = req.nextUrl.clone();
  url.pathname = "/login";
  url.search = new URLSearchParams({ next: path + req.nextUrl.search, ...(s ? { need } : {}) }).toString();
  return NextResponse.redirect(url);
}

export const config = {
  // everything but Next's own static files
  matcher: ["/((?!_next/static|_next/image|favicon.ico|robots.txt|icon.svg|apple-icon.png|manifest.webmanifest|sitemap.xml).*)"],
};
