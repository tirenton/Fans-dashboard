import Link from "next/link";
import type { CSSProperties, ReactNode } from "react";

export function memberColor(m: string) {
  const known = ["LILY", "HAEWON", "SULLYOON", "BAE", "JIWOO", "KYUJIN"];
  return known.includes(m) ? `var(--m-${m.toLowerCase()})` : "var(--m-other)";
}

export function MemberBadge({ member }: { member: string }) {
  return (
    <span className="badge">
      <span className="dot" style={{ "--c": memberColor(member) } as CSSProperties} aria-hidden />
      {member}
    </span>
  );
}

export function Chip({ href, active, children, color }: { href: string; active: boolean; children: ReactNode; color?: string }) {
  return (
    <Link className="chip" href={href} aria-current={active ? "true" : undefined} scroll={false}>
      {color && <span className="dot" style={{ "--c": color } as CSSProperties} aria-hidden />}
      {children}
    </Link>
  );
}

export function Tile({ label, value, sub }: { label: string; value: ReactNode; sub?: ReactNode }) {
  return (
    <div className="card tile">
      <div className="label">{label}</div>
      <div className="value">{value}</div>
      {sub && <div className="sub">{sub}</div>}
    </div>
  );
}

export function ApiDown({ error }: { error: unknown }) {
  return (
    <div className="card empty">
      <strong>Can&apos;t reach the API.</strong>
      <p>
        Is the <code>api</code> container running? ({String((error as Error)?.message ?? error)})
      </p>
    </div>
  );
}

/** Build a query string, dropping empty values. */
export function qs(params: Record<string, string | undefined>) {
  const p = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v) p.set(k, v);
  const s = p.toString();
  return s ? `?${s}` : "";
}
