"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import type { Feed, FeedPost, PullStatus } from "@/lib/api";
import { MemberBadge } from "@/components/ui";
import { LocalTime, Relative } from "@/components/client";

const POLL_MS = 30_000; // the API rescans captures every 30 s; this never reaches FANS

function byNewest(a: FeedPost, b: FeedPost) {
  if (a.createdAt !== b.createdAt) return a.createdAt < b.createdAt ? 1 : -1;
  return a.id < b.id ? 1 : a.id > b.id ? -1 : 0;
}

function merge(list: FeedPost[], incoming: FeedPost[]) {
  const map = new Map(list.map((p) => [p.id, p]));
  for (const p of incoming) map.set(p.id, p); // newer capture wins: counts and likes update in place
  return [...map.values()].sort(byNewest);
}

// canPull: admins only get the Pull now button (it makes the probe send requests to FANS)
export function LiveFeed({ initial, q, artist, from, to, more, canPull = false }: {
  initial: Feed; q?: string; artist?: string; from?: string; to?: string; canPull?: boolean;
  more?: Record<string, string | undefined>; // further /api/feed filters: member, hour, minute, tz
}) {
  const [posts, setPosts] = useState(initial.posts);
  const [total, setTotal] = useState(initial.total);
  const [hasMore, setHasMore] = useState(initial.hasMore);
  const [lastCapture, setLastCapture] = useState(initial.lastCaptureAt);
  const [checkedAt, setCheckedAt] = useState<string | undefined>();
  const [error, setError] = useState<string | null>(null);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const known = useRef(new Set(initial.posts.map((p) => p.id)));
  const [fresh, setFresh] = useState<Set<string>>(new Set());
  const pull = usePull(lastCapture);

  const query = useCallback(
    (extra: Record<string, string>) => {
      const p = new URLSearchParams(extra);
      if (q) p.set("q", q);
      if (artist) p.set("artist", artist);
      if (from) p.set("from", from);
      if (to) p.set("to", to);
      for (const [k, v] of Object.entries(more ?? {})) if (v) p.set(k, v);
      return fetch(`/api/feed?${p}`, { cache: "no-store" }).then(async (r) => {
        if (!r.ok) throw new Error(`HTTP ${r.status}`);
        return (await r.json()) as Feed;
      });
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [q, artist, from, to, JSON.stringify(more ?? {})],
  );

  const absorb = useCallback((f: Feed) => {
    const added = f.posts.filter((p) => !known.current.has(p.id)).map((p) => p.id);
    added.forEach((id) => known.current.add(id));
    if (added.length) setFresh((s) => new Set([...s, ...added]));
    setPosts((list) => merge(list, f.posts));
    setTotal(f.total);
    setLastCapture(f.lastCaptureAt);
  }, []);

  // the layout's 60 s server refresh hands us a new `initial`: fold it in rather than reset
  useEffect(() => absorb(initial), [initial, absorb]);

  useEffect(() => {
    let stop = false;
    const tick = async () => {
      if (document.hidden) return;
      try {
        const f = await query({ limit: "50" });
        if (stop) return;
        absorb(f);
        setError(null);
      } catch (e) {
        if (!stop) setError(String((e as Error).message ?? e));
      } finally {
        if (!stop) setCheckedAt(new Date().toISOString());
      }
    };
    const id = setInterval(tick, POLL_MS);
    const onVis = () => !document.hidden && tick();
    document.addEventListener("visibilitychange", onVis);
    return () => {
      stop = true;
      clearInterval(id);
      document.removeEventListener("visibilitychange", onVis);
    };
  }, [query, absorb]);

  // count unseen arrivals in the tab title while the page is in the background
  useEffect(() => {
    const base = "AxisMixx";
    document.title = fresh.size ? `(${fresh.size}) Fan feed · ${base}` : `Fan feed · ${base}`;
  }, [fresh]);

  const loadOlder = async () => {
    const last = posts[posts.length - 1];
    if (!last) return;
    setLoadingOlder(true);
    try {
      const f = await query({ limit: "30", before: last.createdAt, beforeId: last.id });
      f.posts.forEach((p) => known.current.add(p.id));
      setPosts((list) => merge(list, f.posts));
      setHasMore(f.hasMore);
      setError(null);
    } catch (e) {
      setError(String((e as Error).message ?? e));
    } finally {
      setLoadingOlder(false);
    }
  };

  return (
    <>
      <div className="live-bar" aria-live="polite">
        {error ? (
          <span className="status warning">▲ Can&apos;t reach the API ({error})</span>
        ) : (
          <span className="status good"><span className="pulse" aria-hidden />Live</span>
        )}
        <span className="muted small">
          Last capture <b><Relative iso={lastCapture} /></b>
          {checkedAt && <> · checked <Relative iso={checkedAt} /></>}
          {" · "}{total} post{total === 1 ? "" : "s"}{q || artist ? " match" : " seen"}
        </span>
        {canPull && pull.enabled && (
          <button className="chip" type="button" onClick={pull.request} disabled={pull.pending || pull.busy}
            title="Ask the prototype's loop for a deep pass now: it pages back up to 3 pages (150 posts, about 3 hours on a quiet night) to catch artist likes on older posts. At most one pull runs every 30 min (a sooner request waits), at least 2 min after the previous pass, always inside the request budget.">
            {pull.pending ? <>Pull requested <Relative iso={pull.requestedAt} />…</> : pull.busy ? "Requesting…" : "↻ Pull now"}
          </button>
        )}
        {pull.error && <span className="status warning">▲ Pull failed ({pull.error})</span>}
        {fresh.size > 0 && (
          <button className="chip" type="button" onClick={() => setFresh(new Set())}>
            {fresh.size} new · mark read
          </button>
        )}
      </div>

      {posts.length === 0 ? (
        <div className="card empty">
          <strong>{q || artist || from ? "No posts match these filters." : "No fan posts captured yet."}</strong>
          <p>Posts show up here as soon as the prototype&apos;s next capture is read.</p>
        </div>
      ) : (
        <div className="stack">
          {runs(posts).map((r) => <PostRun key={r[0].id} run={r} fresh={fresh} />)}
        </div>
      )}

      {hasMore && (
        <div className="more">
          <button className="chip" type="button" onClick={loadOlder} disabled={loadingOlder}>
            {loadingOlder ? "Loading…" : "Load older posts"}
          </button>
        </div>
      )}
    </>
  );
}

const PULL_POLL_MS = 5_000;

// "Pull now": writes requests/PULL through the API; the prototype loop deletes it when its pass starts.
function usePull(lastCapture?: string) {
  const [st, setSt] = useState<PullStatus>({ pending: false, enabled: false });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const read = useCallback(async () => {
    try {
      const r = await fetch("/api/pull", { cache: "no-store" });
      if (r.ok) setSt(await r.json());
    } catch {}
  }, []);

  useEffect(() => { read(); }, [read, lastCapture]);
  useEffect(() => {
    if (!st.pending) return;
    const id = setInterval(read, PULL_POLL_MS);
    return () => clearInterval(id);
  }, [st.pending, read]);

  const request = async () => {
    setBusy(true);
    setError(null);
    try {
      const r = await fetch("/api/pull", { method: "POST", cache: "no-store" });
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      setSt(await r.json());
    } catch (e) {
      setError(String((e as Error).message ?? e));
    } finally {
      setBusy(false);
    }
  };

  return { ...st, busy, error, request };
}

function initials(name: string) {
  const s = name.trim();
  return s ? [...s][0].toUpperCase() : "?";
}

const hasArtist = (p: FeedPost) => p.likedBy.length > 0 || !!p.artistComment;

const FOLD_MS = 30 * 60e3;

/** Groups a fan's repeat posts: walking newest first, a fan's post leads a group, and their older posts within
 * FOLD_MS of it fold into that group even when other fans posted in between (spam bursts interleave). Posts with
 * artist activity and posts by artists never fold. Returns the leaders in order, each with its folded posts. */
function runs(posts: FeedPost[]): FeedPost[][] {
  const out: FeedPost[][] = [];
  const open = new Map<string, FeedPost[]>(); // author -> their current group
  for (const p of posts) {
    const g = p.author && !p.authorIsArtist && !hasArtist(p) ? open.get(p.author) : undefined;
    if (g && Date.parse(g[0].createdAt) - Date.parse(p.createdAt) <= FOLD_MS) { g.push(p); continue; }
    const fresh = [p];
    out.push(fresh);
    if (p.author && !p.authorIsArtist) open.set(p.author, fresh);
  }
  return out;
}

/** A fan's newest post, with their other posts from the next 30 min folded behind "+N more", so one fan posting
 * 17 times in a burst takes one card. Nothing is filtered out: expanding shows them all. */
function PostRun({ run, fresh }: { run: FeedPost[]; fresh: Set<string> }) {
  const [open, setOpen] = useState(false);
  const name = run[0].author || "this fan";
  return (
    <>
      {(open ? run : run.slice(0, 1)).map((p) => <PostCard key={p.id} p={p} isNew={fresh.has(p.id)} />)}
      {run.length > 1 && (
        <button type="button" className="chip run-toggle" aria-expanded={open} onClick={() => setOpen((v) => !v)}>
          {open ? `Show less from ${name}` : `+${run.length - 1} more from ${name} within 30 min`}
        </button>
      )}
    </>
  );
}

function PostCard({ p, isNew }: { p: FeedPost; isNew: boolean }) {
  const [open, setOpen] = useState(false);
  const long = p.body.length > 360 || p.body.split("\n").length > 7;
  const media = [
    p.videos && `${p.videos} video${p.videos === 1 ? "" : "s"}`,
    p.stickers && `${p.stickers} sticker${p.stickers === 1 ? "" : "s"}`,
  ].filter(Boolean);

  return (
    <article id={`post-${p.id}`} className={`card fpost${isNew ? " is-new" : ""}`}>
      <header className="fpost-head">
        <span className="avatar" aria-hidden>{initials(p.author)}</span>
        <div className="who">
          <span className="author">{p.author || "Fan"}</span>
          {p.authorIsArtist && <span className="tag">Artist</span>}
          {isNew && <span className="tag new">New</span>}
          <div className="muted small">
            <Relative iso={p.createdAt} /> · <LocalTime iso={p.createdAt} seconds />
          </div>
        </div>
        <a className="open" href={p.url} target="_blank" rel="noreferrer">Open on FANS ↗</a>
      </header>

      {p.body ? (
        <div className={`fbody${long && !open ? " clamped" : ""}`}>{p.body}</div>
      ) : (
        <div className="muted">(no text)</div>
      )}
      {long && (
        <button className="linklike" type="button" onClick={() => setOpen((o) => !o)}>
          {open ? "Show less" : "Show more"}
        </button>
      )}

      {p.media.length > 0 && (
        <Gallery media={p.media} url={p.url} eligible={p.likedBy.length > 0 || !!p.artistComment} />
      )}

      {(p.likedBy.length > 0 || p.artistComment) && (
        <div className="artist-row">
          {p.likedBy.length > 0 && (
            <span className="liked">
              <span className="muted small">Liked by</span>
              {p.likedBy.map((m) => (
                <span key={m} className="liked-by">
                  <MemberBadge member={m} />
                  {p.lateBy?.includes(m) && (
                    <span className="tag late" title="Found after a check had already seen this post without the like: the member came back to an older post">late</span>
                  )}
                </span>
              ))}
            </span>
          )}
          {p.artistComment && (
            <div className="comment">
              <MemberBadge member={p.artistComment.member} /> {p.artistComment.body}
              {p.artistComment.at && <span className="muted small"> · <LocalTime iso={p.artistComment.at} withDate={false} seconds /></span>}
            </div>
          )}
        </div>
      )}

      <footer className="times">
        <span><b>{p.likeCount}</b> like{p.likeCount === 1 ? "" : "s"}</span>
        <span><b>{p.commentCount}</b> comment{p.commentCount === 1 ? "" : "s"}</span>
        {media.length > 0 && <span>{media.join(" · ")}</span>}
        {p.status && p.status !== "ACTIVE" && <span className="status warning">{p.status}</span>}
        <span title="Counts are as of the last capture that included this post">
          as of <LocalTime iso={p.lastSeen} withDate={false} />
        </span>
      </footer>
    </article>
  );
}

// The prototype saves photos only for posts with artist activity, and only when started with SAVE_IMAGES=1.
function Gallery({ media, url, eligible }: { media: FeedPost["media"]; url: string; eligible: boolean }) {
  const saved = media.filter((m) => m.saved);
  const waiting = media.length - saved.length;
  return (
    <div className="gallery-wrap">
      {saved.length > 0 && (
        <div className={`gallery n${Math.min(saved.length, 4)}`}>
          {saved.slice(0, 4).map((m, i) => (
            <a key={m.name} href={`/img/${m.name}`} target="_blank" rel="noreferrer" className="shot">
              {/* eslint-disable-next-line @next/next/no-img-element -- local file, served as-is */}
              <img src={`/img/${m.name}`} alt={`Photo ${i + 1} of ${media.length}`} loading="lazy" />
              {i === 3 && saved.length > 4 && <span className="more-n">+{saved.length - 4}</span>}
            </a>
          ))}
        </div>
      )}
      {waiting > 0 && (
        <div className="muted small">
          {waiting} photo{waiting === 1 ? "" : "s"}{" "}
          {eligible ? "not saved yet" : "(only posts with artist activity get saved)"} ·{" "}
          <a href={url} target="_blank" rel="noreferrer">view on FANS ↗</a>
        </div>
      )}
    </div>
  );
}
