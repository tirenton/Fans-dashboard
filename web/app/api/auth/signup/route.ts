// The sign-up form posts here. The Go API keeps it as a pending sign-up and mails a confirm link.
import { sameOrigin } from "@/lib/auth";
import { callAccounts, clientIp, see, turnstileOK } from "@/lib/accounts";

export const dynamic = "force-dynamic";

export async function POST(req: Request) {
  if (!sameOrigin(req)) return Response.json({ error: "forbidden" }, { status: 403 });
  const form = await req.formData().catch(() => null);
  const field = (k: string, max: number) => String(form?.get(k) ?? "").slice(0, max);
  const name = field("name", 64).trim().toLowerCase();
  const back = (error: string) => see(`/signup?${new URLSearchParams({ error, ...(name ? { name } : {}) })}`);
  const password = field("password", 1100);
  if (password !== field("password2", 1100)) return back("mismatch");
  const ip = clientIp(req);
  if (!(await turnstileOK(field("cf-turnstile-response", 2048), ip))) return back("captcha");
  const { error } = await callAccounts("/api/auth/signup", { name, email: field("email", 300), password, ip });
  return error ? back(error) : see("/signup?sent=1");
}
