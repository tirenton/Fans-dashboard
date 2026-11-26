// "Log out": drops the session cookie and goes back to the public Main dashboard.
import { NextResponse } from "next/server";
import { sameOrigin } from "@/lib/auth";
import { SESSION_COOKIE } from "@/lib/session";

export const dynamic = "force-dynamic";

export async function POST(req: Request) {
  if (!sameOrigin(req)) return Response.json({ error: "forbidden" }, { status: 403 });
  const r = new NextResponse(null, { status: 303, headers: { Location: "/", "Cache-Control": "no-store" } });
  r.cookies.set(SESSION_COOKIE, "", { httpOnly: true, sameSite: "lax", path: "/", maxAge: 0 });
  return r;
}
