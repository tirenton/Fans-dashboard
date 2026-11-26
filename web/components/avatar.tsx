// A person's picture, or the first letter of their name on a colour of their own when they have none.
// Plain markup (no hooks), so server and client components can both use it.
import type { CSSProperties } from "react";

export type ProfileInfo = { display?: string; avatar?: number };

/** The name to show: the display name if one is set, else the account name. */
export const shownName = (name: string, p?: ProfileInfo) => p?.display || name;

function hue(name: string) {
  let h = 0;
  for (const c of name) h = (h * 31 + c.codePointAt(0)!) >>> 0;
  return h % 360;
}

export function Avatar({ name, profile, size = 32, className = "" }: { name: string; profile?: ProfileInfo; size?: number; className?: string }) {
  const style = { width: size, height: size } as CSSProperties;
  if (profile?.avatar) {
    // eslint-disable-next-line @next/next/no-img-element -- a small private image behind the login, no optimiser
    return <img className={`avatar ${className}`} src={`/api/avatar/${name}?v=${profile.avatar}`} alt="" width={size} height={size} style={style} loading="lazy" />;
  }
  const letter = (Array.from(shownName(name, profile).trim())[0] ?? "?").toUpperCase();
  return (
    <span
      className={`avatar avatar-letter ${className}`} aria-hidden
      style={{ ...style, fontSize: Math.round(size * 0.44), "--h": hue(name) } as CSSProperties}
    >
      {letter}
    </span>
  );
}
