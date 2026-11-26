// The new-password form on /reset posts here with the link's token.
import { sameOrigin } from "@/lib/auth";
import { callAccounts, see } from "@/lib/accounts";

export const dynamic = "force-dynamic";

export async function POST(req: Request) {
  if (!sameOrigin(req)) return Response.json({ error: "forbidden" }, { status: 403 });
  const form = await req.formData().catch(() => null);
  const token = String(form?.get("t") ?? "").slice(0, 100);
  const password = String(form?.get("password") ?? "").slice(0, 1100);
  const back = (error: string) => see(`/reset?${new URLSearchParams(error === "token" ? { error } : { t: token, error })}`);
  if (password !== String(form?.get("password2") ?? "").slice(0, 1100)) return back("mismatch");
  const { error } = await callAccounts("/api/auth/reset", { token, password });
  return error ? back(error) : see("/login?reset=1");
}
