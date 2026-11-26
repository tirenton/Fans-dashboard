// Login sessions: an HttpOnly cookie "<payload>.<signature>". payload = base64url JSON {n: name, r: role, exp},
// signature = HMAC-SHA256(SESSION_SECRET). Web Crypto only, so proxy.ts, pages and route handlers can all check it.
// Passwords never reach this app's storage: the Go API checks them against argon2id hashes.

export type Role = "admin" | "user";
export type Session = { name: string; role: Role; exp: number };

export const SESSION_COOKIE = "nmx_session";
export const SESSION_DAYS = 7;

const enc = new TextEncoder();
let keyFor: { secret: string; key: Promise<CryptoKey> } | null = null;

function hmacKey(): Promise<CryptoKey> | null {
  const secret = process.env.SESSION_SECRET ?? "";
  if (secret.length < 32) return null; // not set up: nobody is signed in
  if (keyFor?.secret !== secret) {
    keyFor = { secret, key: crypto.subtle.importKey("raw", enc.encode(secret), { name: "HMAC", hash: "SHA-256" }, false, ["sign", "verify"]) };
  }
  return keyFor.key;
}

function b64url(bytes: Uint8Array) {
  let s = "";
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}
function fromB64url(s: string) {
  const bin = atob(s.replace(/-/g, "+").replace(/_/g, "/"));
  return Uint8Array.from(bin, (c) => c.charCodeAt(0));
}

export async function signSession(s: Session): Promise<string | null> {
  const key = hmacKey();
  if (!key) return null;
  const payload = b64url(enc.encode(JSON.stringify({ n: s.name, r: s.role, exp: s.exp })));
  const sig = new Uint8Array(await crypto.subtle.sign("HMAC", await key, enc.encode(payload)));
  return `${payload}.${b64url(sig)}`;
}

/** The session in a cookie value, or null when it is missing, forged, expired or the secret is not set. */
export async function verifySession(token?: string): Promise<Session | null> {
  const key = hmacKey();
  if (!key || !token || token.length > 1024) return null;
  const [payload, sig, extra] = token.split(".");
  if (!payload || !sig || extra !== undefined) return null;
  try {
    if (!(await crypto.subtle.verify("HMAC", await key, fromB64url(sig), enc.encode(payload)))) return null;
    const v = JSON.parse(new TextDecoder().decode(fromB64url(payload)));
    if ((v.r !== "admin" && v.r !== "user") || typeof v.n !== "string" || typeof v.exp !== "number" || v.exp < Date.now()) return null;
    return { name: v.n, role: v.r, exp: v.exp };
  } catch {
    return null;
  }
}

/** Whether a role may see something that needs `need`: admin may do everything a user may. */
export const allows = (role: Role | undefined, need: Role) => role === "admin" || (need === "user" && role === "user");

/** A same-site path to go to after logging in; anything else (other sites, "//host") becomes "/". */
export function safeNext(v: string | null | undefined) {
  if (!v || !v.startsWith("/") || v.startsWith("//") || v.startsWith("/\\") || v.startsWith("/login") || v.startsWith("/api/")) return "/";
  return v.slice(0, 500);
}
