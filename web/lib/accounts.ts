// Helpers for the sign-up, confirm, forgot and reset forms (pages under app/, route handlers under app/api/auth/).
import { NextResponse } from "next/server";
import { apiFetch } from "@/lib/api";

/** Whether this server takes sign-ups (the Go API has mail and SITE_URL set up). */
export async function signupOpen(): Promise<boolean> {
  try {
    const res = await apiFetch("/api/auth/options");
    return res.ok && (await res.json()).signup === true;
  } catch {
    return false;
  }
}

/** A relative redirect, so it stays right behind any reverse proxy. */
export const see = (to: string) => new NextResponse(null, { status: 303, headers: { Location: to, "Cache-Control": "no-store" } });

/** The visitor's address as Caddy passed it on ("" when unknown). */
export const clientIp = (req: Request) => (req.headers.get("x-forwarded-for") ?? "").split(",")[0].trim().slice(0, 64);

/** POSTs a form's fields to the Go API; returns "" on success, else an error code (see api/account.go). */
export async function callAccounts(path: string, body: Record<string, string>): Promise<{ error: string; data?: Record<string, string> }> {
  try {
    const res = await apiFetch(path, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) });
    const data = await res.json().catch(() => ({}));
    if (res.ok) return { error: "", data };
    if (res.status === 404) return { error: "off" };
    return { error: typeof data?.error === "string" ? data.error : "down" };
  } catch {
    return { error: "down" };
  }
}

// Cloudflare Turnstile on the sign-up form: on when both keys are set (TURNSTILE_SITE_KEY, TURNSTILE_SECRET_KEY).
export const turnstileSiteKey = () => (process.env.TURNSTILE_SECRET_KEY ? process.env.TURNSTILE_SITE_KEY ?? "" : "");

export async function turnstileOK(token: string, ip: string): Promise<boolean> {
  const secret = process.env.TURNSTILE_SECRET_KEY;
  if (!secret || !process.env.TURNSTILE_SITE_KEY) return true;
  if (!token || token.length > 2048) return false;
  try {
    const res = await fetch("https://challenges.cloudflare.com/turnstile/v0/siteverify", {
      method: "POST",
      body: new URLSearchParams({ secret, response: token, ...(ip ? { remoteip: ip } : {}) }),
      signal: AbortSignal.timeout(10_000),
    });
    return res.ok && (await res.json()).success === true;
  } catch {
    return false;
  }
}

/** What each error code means on the forms. */
export const ACCOUNT_ERRORS: Record<string, string> = {
  name: "Names are 3–32 characters: lowercase letters, digits, dot, dash or underscore. A few names, like admin, are reserved.",
  email: "That doesn't look like an email address.",
  password: "Passwords need at least 10 characters.",
  mismatch: "The two passwords are different.",
  taken: "That name is taken. Pick another one.",
  emailtaken: "That email address already has an account. Log in, or reset its password.",
  token: "This link has expired or was already used.",
  fast: "Too many tries from your network. Wait an hour and try again.",
  captcha: "Please complete the check above the button.",
  off: "Sign-up isn't open on this server.",
  down: "That didn't work. Try again in a minute.",
};
