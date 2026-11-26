"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { Avatar, shownName, type ProfileInfo } from "@/components/avatar";
import { fmtDuration } from "@/lib/format";

// who sees which link: null = everyone (the public Main dashboard)
const LINKS: { href: string; label: string; need: "user" | "admin" | null }[] = [
  { href: "/", label: "Main dashboard", need: null },
  { href: "/feed", label: "Fan feed", need: "admin" },
  { href: "/admin", label: "Admin", need: "admin" },
];

// Admin shows on the VPS too (controls false): its probe controls live on the home PC's own dashboard, but the
// totals, probe health, visitors and feedback are there
// eslint-disable-next-line @typescript-eslint/no-unused-vars
export function Nav({ role, controls = true }: { role?: "admin" | "user"; controls?: boolean }) {
  const path = usePathname();
  const links = LINKS.filter((l) => !l.need || role === "admin" || (l.need === "user" && role === "user"));
  return (
    <nav className="nav" aria-label="Main">
      {links.map((l) => (
        <Link key={l.href} href={l.href} aria-current={path === l.href ? "page" : undefined}>
          {l.label}
        </Link>
      ))}
    </nav>
  );
}

/** Signed in: picture and name (to the profile page) and Log out. Signed out: a Log in link back to this page. */
export function Account({ name, profile }: { name?: string; profile?: ProfileInfo }) {
  const path = usePathname();
  if (!name) {
    if (path === "/login") return null;
    return <Link className="chip account" href={path === "/" ? "/login" : `/login?next=${encodeURIComponent(path)}`}>Log in</Link>;
  }
  return (
    <form className="account" method="post" action="/api/auth/logout">
      <Link href="/profile" className="account-me" title="Your profile" aria-current={path === "/profile" ? "page" : undefined}>
        <Avatar name={name} profile={profile} size={26} />
        <span className="small me-name">{shownName(name, profile)}</span>
      </Link>
      <button type="submit" className="chip">Log out</button>
    </form>
  );
}

// The day (as the counter told it) this browser last visited, so the counter can tell unique visitors over 7 and 30
// days without keeping anything about them. Kept in memory too, for browsers that block storage.
const VISIT_KEY = "nmixx.lastVisit";
let lastVisit = "";

/** Reports each page view to the visitor counter (api/visits.go): the path, the day of this browser's last visit,
 *  and on the first page only the site that linked here. A script, so most bots (which don't run scripts) aren't
 *  counted. */
export function VisitBeacon() {
  const path = usePathname();
  const [first, setFirst] = useState(true);
  useEffect(() => {
    let ref = "";
    if (first) {
      try {
        const r = document.referrer ? new URL(document.referrer).hostname : "";
        if (r && r !== location.hostname) ref = r;
      } catch {}
      setFirst(false);
    }
    let last = lastVisit;
    try { last = localStorage.getItem(VISIT_KEY) ?? last; } catch {}
    fetch("/api/visit", {
      method: "POST", keepalive: true, headers: { "Content-Type": "application/json" }, body: JSON.stringify({ path, ref, last }),
    })
      .then((r) => (r.ok ? r.json() : null))
      .then((b) => {
        if (typeof b?.day !== "string" || !b.day) return; // not counted (e.g. an admin): keep what we had
        lastVisit = b.day;
        try { localStorage.setItem(VISIT_KEY, b.day); } catch {}
      })
      .catch(() => {});
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once per page, not when `first` flips
  }, [path]);
  return null;
}

/** Re-runs the server components every `seconds`, so the page follows new captures. */
export function AutoRefresh({ seconds = 60 }: { seconds?: number }) {
  const router = useRouter();
  useEffect(() => {
    const id = setInterval(() => router.refresh(), seconds * 1000);
    return () => clearInterval(id);
  }, [router, seconds]);
  return <span className="refresh-note">Auto-refreshes every {seconds}s</span>;
}

export function useNow(tickMs = 30_000) {
  const [now, setNow] = useState<number | null>(null);
  useEffect(() => {
    setNow(Date.now());
    const id = setInterval(() => setNow(Date.now()), tickMs);
    return () => clearInterval(id);
  }, [tickMs]);
  return now;
}

/** Absolute time in the viewer's own time zone (rendered after mount to avoid a server/client mismatch). */
// seconds: for exact times from FANS (post and comment times); estimates and capture times stay at minutes.
export function LocalTime({ iso, withDate = true, seconds = false }: { iso?: string; withDate?: boolean; seconds?: boolean }) {
  const [text, setText] = useState<string | null>(null);
  useEffect(() => {
    if (!iso) return;
    const d = new Date(iso);
    const time: Intl.DateTimeFormatOptions = { hour: "2-digit", minute: "2-digit", ...(seconds ? { second: "2-digit" } : {}) };
    setText(d.toLocaleString(undefined, withDate ? { month: "short", day: "numeric", ...time } : time));
  }, [iso, withDate, seconds]);
  if (!iso) return <span>—</span>;
  return <time dateTime={iso} title={iso}>{text ?? iso.slice(0, seconds ? 19 : 16).replace("T", " ") + " UTC"}</time>;
}

/** "in 3 h 10 min" / "12 min ago" relative to the viewer's clock. */
export function Relative({ iso }: { iso?: string }) {
  const now = useNow();
  if (!iso) return <span>—</span>;
  if (now === null) return <span>…</span>;
  const diff = new Date(iso).getTime() - now;
  return <span title={iso}>{diff >= 0 ? `in ${fmtDuration(diff)}` : `${fmtDuration(diff)} ago`}</span>;
}

/** Status pill for whether the prototype is alive, judged against the viewer's clock. */
export function Freshness({ lastPassAt, stopped }: { lastPassAt?: string; stopped: boolean }) {
  const now = useNow();
  if (stopped) return <span className="status critical">■ Stopped</span>;
  if (!lastPassAt || now === null) return <span className="status warning">▲ No passes yet</span>;
  const age = now - new Date(lastPassAt).getTime();
  if (age > 20 * 60_000) return <span className="status warning">▲ Quiet for {fmtDuration(age)}</span>;
  return <span className="status good">● Running</span>;
}
