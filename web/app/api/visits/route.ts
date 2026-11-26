// The visitor counter's summary, for the Admin page.
import { apiFetch } from "@/lib/api";
import { denyUnless } from "@/lib/auth";
import { viaApi } from "@/lib/forward";

export const dynamic = "force-dynamic";

export async function GET() {
  const denied = await denyUnless("admin");
  if (denied) return denied;
  return viaApi(() => apiFetch("/api/visits"));
}
