// The Confirm button on /confirm posts the link's token here; the Go API turns the sign-up into an account.
import { sameOrigin } from "@/lib/auth";
import { callAccounts, see } from "@/lib/accounts";

export const dynamic = "force-dynamic";

export async function POST(req: Request) {
  if (!sameOrigin(req)) return Response.json({ error: "forbidden" }, { status: 403 });
  const form = await req.formData().catch(() => null);
  const token = String(form?.get("t") ?? "").slice(0, 100);
  const { error, data } = await callAccounts("/api/auth/confirm", { token });
  if (error) return see(`/confirm?${new URLSearchParams({ error })}`);
  return see(`/login?${new URLSearchParams({ confirmed: data?.name ?? "" })}`);
}
