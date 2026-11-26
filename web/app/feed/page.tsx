import { api, isMember, MEMBERS, type FanDay, type Feed, type Stats } from "@/lib/api";
import { ApiDown, Chip, memberColor, qs } from "@/components/ui";
import { LiveFeed } from "@/components/live-feed";
import { FanHours } from "@/components/fan-hours";
import { SearchTrigger } from "@/components/search";
import { ZONES } from "@/lib/zones";
import { requireRole } from "@/lib/auth";
import { probeControls } from "@/lib/features";

export const dynamic = "force-dynamic";

// artist=1 is the artist view (it replaced the Artist activity page): posts an NMIXX member liked or commented on,
// optionally one member's, at a time range or an hour (and minute) of day, as the fan chart's artist strip links.
type Search = Promise<{
  q?: string; artist?: string; member?: string; tz?: string; from?: string; to?: string; hour?: string; minute?: string;
}>;
const isoOrUndef = (v?: string) => (v && !Number.isNaN(Date.parse(v)) ? new Date(v).toISOString() : undefined);
const intIn = (v: string | undefined, max: number) => {
  const n = v !== undefined && /^\d{1,2}$/.test(v) ? Number(v) : NaN;
  return n >= 0 && n <= max ? String(n) : undefined;
};
const pad = (v: string) => v.padStart(2, "0");

export default async function FeedPage({ searchParams }: { searchParams: Search }) {
  const session = await requireRole("admin", "/feed");
  const canPull = session.role === "admin" && probeControls();
  const sp = await searchParams;
  const q = (sp.q ?? "").trim().slice(0, 100) || undefined;
  const member = isMember(sp.member) ? sp.member : undefined;
  const artist = sp.artist === "1" || member ? "1" : undefined;
  const zone = ZONES.find((z) => z.id === sp.tz) ?? ZONES[0];
  const tz = zone === ZONES[0] ? undefined : zone.id;
  // a time range picked on the Past 24 hours chart (double-click a minute), or an hour (and minute) of day
  const from = isoOrUndef(sp.from), to = from ? isoOrUndef(sp.to) : undefined;
  const hour = from ? undefined : intIn(sp.hour, 23);
  const minute = hour ? intIn(sp.minute, 59) : undefined;
  const hourLabel = hour && (minute ? `${pad(hour)}:${pad(minute)}` : `${pad(hour)}:00–${pad(hour)}:59`);
  // what every link on this page keeps (each link overrides what it changes)
  const keep = { q, artist, member, tz, from, to, hour, minute };
  const fmtT = (iso: string) => new Intl.DateTimeFormat("en-GB", { timeZone: zone.id, hourCycle: "h23", weekday: "short", hour: "2-digit", minute: "2-digit" }).format(new Date(iso));
  // the fan chart is extra: if its stats call fails, the feed still shows
  const [stats, days] = await Promise.all([
    api<Stats>(`/api/stats${qs({ tz: zone.id })}`).catch(() => null),
    api<FanDay[]>(`/api/fan-days${qs({ tz: zone.id })}`).catch(() => null), // the Average chart's day calendar
  ]);

  const more = { member, hour, minute, tz: hour ? zone.id : undefined };
  let feed: Feed;
  try {
    feed = await api<Feed>(`/api/feed${qs({ limit: "30", q, artist, from, to, ...more })}`);
  } catch (e) {
    return (<><h1>Fan feed</h1><ApiDown error={e} /></>);
  }

  return (
    <>
      <h1>Fan feed</h1>
      <p className="lede">
        Fan posts on the NMIXX FANS feed, newest first, as the prototype captures them. New posts appear here within
        about 30 s of a capture; captures happen about every 30 min, every ~10 min while an artist is active, so that is how live this feed can be.
        {canPull && <> <b>Pull now</b> asks the prototype&apos;s
        running loop for a deep pass (up to 150 posts back), which is useful when an artist is online and liking
        older posts. At most one pull runs every 30 min; a sooner request waits its turn.</>}
      </p>

      {stats && (
        <section className="card" style={{ marginBottom: 16 }}>
          <h2>Fan posts by hour of day · {zone.label}</h2>
          <div className="filters">
            {ZONES.map((z) => (
              <Chip key={z.id} href={`/feed${qs({ q, artist, member, tz: z === ZONES[0] ? undefined : z.id })}`} active={zone === z}>{z.label}</Chip>
            ))}
          </div>
          <FanHours fans={stats.fans} members={stats.members} days={days ?? []} start={stats.timelineStart} tz={zone.id} tzLabel={zone.label} />
        </section>
      )}

      <div className="filters">
        <SearchTrigger q={q} tab="posts" placeholder="Search posts, nicknames or artist comments" />
        {q && <Chip href={`/feed${qs({ ...keep, q: undefined })}`} active={false}>× Clear search</Chip>}
        <span className="sep" aria-hidden />
        <Chip href={`/feed${qs({ ...keep, artist: undefined, member: undefined })}`} active={!artist}>All posts</Chip>
        <Chip href={`/feed${qs({ ...keep, artist: "1", member: undefined })}`} active={!!artist && !member}>With artist activity</Chip>
      </div>

      {artist && (
        <div className="filters" role="group" aria-label="Member">
          {MEMBERS.map((m) => (
            <Chip key={m} href={`/feed${qs({ ...keep, member: member === m ? undefined : m })}`} active={member === m} color={memberColor(m)}>{m}</Chip>
          ))}
          <span className="muted small">Posts the members liked or commented on; a like counts at its post&apos;s time.</span>
        </div>
      )}

      {(from || hourLabel) && (
        <div className="filters range-note" id="feed-range">
          <span className="status">
            {from ? <>Showing posts from <b>{fmtT(from)}</b>{to && <> to <b>{fmtT(to)}</b></>}</>
              : <>Showing {artist ? "artist activity" : "posts"} at <b>{hourLabel}</b> on any day</>} ({zone.label})
          </span>
          <Chip href={`/feed${qs({ ...keep, from: undefined, to: undefined, hour: undefined, minute: undefined })}`} active={false}>× Show all times</Chip>
        </div>
      )}

      <LiveFeed canPull={canPull} key={`${q ?? ""}|${artist ?? ""}|${member ?? ""}|${from ?? ""}|${to ?? ""}|${hour ?? ""}|${minute ?? ""}`}
        initial={feed} q={q} artist={artist} from={from} to={to} more={more} />
    </>
  );
}
