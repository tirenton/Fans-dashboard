// The login form posts here. The Go API checks the password against its argon2id hash; on success this sets
// the signed session cookie and goes on to the page that asked for a login.
import { NextResponse } from "next/server";
import { apiFetch } from "@/lib/api";
import { sameOrigin } from "@/lib/auth";
import { safeNext, SESSION_COOKIE, SESSION_DAYS, signSession, type Role } from "@/lib/session";

export const dynamic = "force-dynamic";

/** A relative redirect, so it stays right behind any reverse proxy. */
const see = (to: string) => new NextResponse(null, { status: 303, headers: { Location: to, "Cache-Control": "no-store" } });

export async function POST(req: Request) {
  if (!sameOrigin(req)) return Response.json({ error: "forbidden" }, { status: 403 });
  const form = await req.formData().catch(() => null);
  const name = String(form?.get("name") ?? "").slice(0, 254); // a name, or the account's email
  const password = String(form?.get("password") ?? "").slice(0, 1024);
  const next = safeNext(String(form?.get("next") ?? "/"));
  const back = (error: string) => see(`/login?${new URLSearchParams({ error, ...(next !== "/" ? { next } : {}) })}`);
  if (!name || !password) return back("bad");
  if ((process.env.SESSION_SECRET ?? "").length < 32) return back("setup");

  let res: Response;
  try {
    const ip = (req.headers.get("x-forwarded-for") ?? "").split(",")[0].trim().slice(0, 64);
    res = await apiFetch("/api/auth/login", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ name, password, ip }),
    });
  } catch {
    return back("down");
  }
  if (res.status === 429) return back("locked");
  if (res.status === 401) return back("bad");
  if (!res.ok) return back("down");
  const u = (await res.json()) as { name: string; role: Role };
  const token = await signSession({ name: u.name, role: u.role, exp: Date.now() + SESSION_DAYS * 864e5 });
  if (!token) return back("setup");

  const r = see(next);
  r.cookies.set(SESSION_COOKIE, token, {
    httpOnly: true, sameSite: "lax", path: "/", maxAge: SESSION_DAYS * 86400,
    secure: process.env.COOKIE_SECURE === "1" || req.headers.get("x-forwarded-proto") === "https" || new URL(req.url).protocol === "https:",
  });
  return r;
}
