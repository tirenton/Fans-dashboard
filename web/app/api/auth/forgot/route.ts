// The Forgot password form posts here. The answer is the same whether or not the email has an account.
import { sameOrigin } from "@/lib/auth";
import { callAccounts, clientIp, see } from "@/lib/accounts";

export const dynamic = "force-dynamic";

export async function POST(req: Request) {
  if (!sameOrigin(req)) return Response.json({ error: "forbidden" }, { status: 403 });
  const form = await req.formData().catch(() => null);
  const email = String(form?.get("email") ?? "").slice(0, 300);
  const { error } = await callAccounts("/api/auth/forgot", { email, ip: clientIp(req) });
  return see(error ? `/forgot?${new URLSearchParams({ error })}` : "/forgot?sent=1");
}
