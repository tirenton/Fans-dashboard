import type { Metadata } from "next";
import "./globals.css";
import type { ProfileInfo } from "@/components/avatar";
import { Account, AutoRefresh, Nav, VisitBeacon } from "@/components/client";
import { api } from "@/lib/api";
import { SearchPalette } from "@/components/search";
import { getSession } from "@/lib/auth";
import { probeControls } from "@/lib/features";
import { SITE_URL } from "@/lib/site";

const description = "When NMIXX members like and comment on fan posts on FANS, and when fans post.";

// The site's name wherever a browser, app or link preview shows one instead of the address
// (with app/manifest.ts for installed apps and home-screen icons).
export const metadata: Metadata = {
  metadataBase: new URL(SITE_URL),
  title: "AxisMixx · NMIXX FANS Statistics",
  description,
  applicationName: "AxisMixx",
  appleWebApp: { title: "AxisMixx", capable: true, statusBarStyle: "black" },
  openGraph: { siteName: "AxisMixx", title: "AxisMixx · NMIXX FANS Statistics", description, type: "website" },
  twitter: { card: "summary", title: "AxisMixx", description },
};

export const viewport = { themeColor: "#0d0d0e" };

export default async function RootLayout({ children }: { children: React.ReactNode }) {
  const session = await getSession();
  let profile: ProfileInfo | undefined;
  if (session) profile = await api<ProfileInfo>(`/api/profile/${session.name}`).catch(() => undefined);
  return (
    <html lang="en">
      <body>
        <header className="topbar">
          <div className="topbar-inner">
            <a className="brand" href="/" aria-label="AxisMixx · Main dashboard">
              <img src="/icon.svg" alt="" width={28} height={28} className="brand-logo" />
              <span className="brand-name">AxisMixx</span>
              <span className="brand-sub">NMIXX FANS statistics</span>
            </a>
            <Nav role={session?.role} controls={probeControls()} />
            {session?.role === "admin" && <SearchPalette />}
            <AutoRefresh seconds={60} />
            <Account name={session?.name} profile={profile} />
          </div>
        </header>
        <main className="shell">{children}</main>
        <VisitBeacon />
      </body>
    </html>
  );
}
