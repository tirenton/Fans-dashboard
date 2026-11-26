// Main dashboard: the one page anyone can open without logging in. The Members charts plus Fan posts by hour of
// day, read-only: no search, no fan posts or names, and no links into the locked pages (minute-by-minute zoom works).
// Below them, for anyone with an account: the open chat, and the feedback box at the very bottom.
import type { ProfileInfo } from "@/components/avatar";
import { Chat, FeedbackForm } from "@/components/chat";
import { MembersView, type MembersSearch } from "@/components/members-view";
import { signupOpen } from "@/lib/accounts";
import { api } from "@/lib/api";
import { getSession } from "@/lib/auth";
import { SITE_URL } from "@/lib/site";

export const dynamic = "force-dynamic";
// the filter links (?member=…, ?month=…) are the same page to a search engine
export const metadata = { alternates: { canonical: "/" } };

export default async function DashboardPage({ searchParams }: { searchParams: Promise<MembersSearch> }) {
  const [sp, s, open] = await Promise.all([searchParams, getSession(), signupOpen()]);
  const profile = s ? await api<ProfileInfo>(`/api/profile/${s.name}`).catch(() => undefined) : undefined;
  const me = s ? { name: s.name, role: s.role, profile } : null;
  return (
    <>
      {/* the site name Google shows above search results (instead of the address) */}
      <script type="application/ld+json" dangerouslySetInnerHTML={{ __html: JSON.stringify({
        "@context": "https://schema.org", "@type": "WebSite", name: "AxisMixx",
        alternateName: "NMIXX FANS Statistics", url: `${SITE_URL}/`,
      }) }} />
      <MembersView sp={sp} base="/" title="Main dashboard" fans lede={<>
        When NMIXX members like and comment on fan posts on FANS, and when fans post, from what has been
        captured. Hover any chart for details; double-click an hour of the fan chart to see it minute by minute.
      </>} />
      <Chat me={me} signupOpen={open} />
      <FeedbackForm me={me} />
    </>
  );
}
