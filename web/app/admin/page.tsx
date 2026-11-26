import { AdminPanel } from "@/components/admin";
import { AdminStats } from "@/components/admin-stats";
import { FeedbackList } from "@/components/chat";
import { HealthSection } from "@/components/health-section";
import { VisitsPanel } from "@/components/visits";
import { requireRole } from "@/lib/auth";
import { probeControls } from "@/lib/features";

export const dynamic = "force-dynamic";

export default async function AdminPage() {
  await requireRole("admin", "/admin");
  if (!probeControls()) {
    return (
      <>
        <h1>Admin</h1>
        <p className="lede">
          The probe runs on the home PC, so its controls (start, pause, log in to FANS, resume, Pull now) are on that
          PC&apos;s own dashboard at <code>http://localhost:3000/admin</code>. This server only shows what the PC uploads;
          Probe health below shows when the last capture arrived.
        </p>
        <AdminStats />
        <HealthSection />
        <VisitsPanel />
        <FeedbackList />
      </>
    );
  }
  return (
    <>
      <h1>Admin</h1>
      <p className="lede">
        Controls for the Playwright probe on the home PC. The dashboard never contacts FANS: it leaves a note in the
        probe&apos;s requests folder (from the VPS, through the PC&apos;s dashboard), and the probe picks it up within a few
        seconds (settings apply from its next pass).
      </p>
      <AdminPanel />
      <AdminStats />
      <HealthSection />
      <VisitsPanel />
      <FeedbackList />
    </>
  );
}
