"use client";
// Search popup (Ctrl+K or "/"): one box searching posts, fans and artist activity, with live results in tabs.
// Pages open it through <SearchTrigger>, which looks like a search field.
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useRouter } from "next/navigation";
import type { ArtistEvent, FanMatch, FeedPost, SearchResults } from "@/lib/api";
import { MemberBadge } from "@/components/ui";
import { fmtDuration } from "@/lib/format";

export type SearchTab = "posts" | "fans" | "activity";
const TABS: { id: SearchTab; label: string }[] = [
  { id: "posts", label: "Posts" },
  { id: "fans", label: "Fans" },
  { id: "activity", label: "Artist activity" },
];
const OPEN_EVENT = "nmixx:search";

/** Opens the search popup, optionally with a query and tab. */
export function openSearch(q = "", tab?: SearchTab) {
  window.dispatchEvent(new CustomEvent(OPEN_EVENT, { detail: { q, tab } }));
}

const ago = (iso: string) => `${fmtDuration(Date.now() - Date.parse(iso))} ago`;
const initial = (s: string) => (s ? [...s][0].toUpperCase() : "?");

/** Wraps each case-insensitive match of q in <mark>. */
function Hl({ text, q }: { text: string; q: string }) {
  if (!q) return <>{text}</>;
  const re = new RegExp(`(${q.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")})`, "gi");
  return <>{text.split(re).map((part, i) => (i % 2 ? <mark key={i}>{part}</mark> : part))}</>;
}

function SearchIcon() {
  return (
    <svg className="sp-icon" viewBox="0 0 24 24" width="18" height="18" aria-hidden fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
      <circle cx="11" cy="11" r="7" /><path d="m20 20-3.5-3.5" />
    </svg>
  );
}

/** Looks like a search field; opens the popup. Shows the page's current search, if any. */
export function SearchTrigger({ q, tab, placeholder }: { q?: string; tab: SearchTab; placeholder: string }) {
  return (
    <button type="button" className="sp-trigger" onClick={() => openSearch(q ?? "", tab)} aria-label={`Search. ${placeholder}`}>
      <SearchIcon />
      <span className={q ? "" : "muted"}>{q || placeholder}</span>
      <kbd>Ctrl K</kbd>
    </button>
  );
}

type Item = { key: string; href: string; node: ReactNode };

export function SearchPalette() {
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [q, setQ] = useState("");
  const [tab, setTab] = useState<SearchTab>("posts");
  const [res, setRes] = useState<SearchResults | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [active, setActive] = useState(0);
  const input = useRef<HTMLInputElement>(null);

  // open: button, Ctrl/Cmd+K, "/" outside text fields, or openSearch() from a page
  useEffect(() => {
    const onOpen = (e: Event) => {
      const d = (e as CustomEvent<{ q?: string; tab?: SearchTab }>).detail ?? {};
      setQ(d.q ?? ""); if (d.tab) setTab(d.tab); setOpen(true);
    };
    const onKey = (e: KeyboardEvent) => {
      const typing = e.target instanceof HTMLElement && (e.target.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(e.target.tagName));
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") { e.preventDefault(); setOpen((o) => !o); }
      else if (e.key === "/" && !typing) { e.preventDefault(); setOpen(true); }
    };
    window.addEventListener(OPEN_EVENT, onOpen);
    window.addEventListener("keydown", onKey);
    return () => { window.removeEventListener(OPEN_EVENT, onOpen); window.removeEventListener("keydown", onKey); };
  }, []);

  useEffect(() => {
    if (!open) return;
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    requestAnimationFrame(() => input.current?.select());
    return () => { document.body.style.overflow = prev; };
  }, [open]);

  // live results, 200 ms after typing stops
  useEffect(() => {
    if (!open) return;
    const term = q.trim();
    if (!term) { setRes(null); setError(null); setLoading(false); return; }
    const ctl = new AbortController();
    setLoading(true);
    const t = setTimeout(() => {
      fetch(`/api/search?q=${encodeURIComponent(term)}`, { signal: ctl.signal, cache: "no-store" })
        .then(async (r) => { if (!r.ok) throw new Error(`HTTP ${r.status}`); return (await r.json()) as SearchResults; })
        .then((d) => { setRes(d); setError(null); setActive(0); setLoading(false); })
        .catch((e) => { if (e.name !== "AbortError") { setError(String(e.message ?? e)); setLoading(false); } });
    }, 200);
    return () => { clearTimeout(t); ctl.abort(); };
  }, [q, open]);

  const term = q.trim();
  const enc = encodeURIComponent(term);
  const allHref = tab === "activity" ? `/feed?artist=1&q=${enc}` : `/feed?q=${enc}`;

  const items: Item[] = useMemo(() => {
    if (!res) return [];
    if (tab === "posts") return res.posts.items.map((p: FeedPost) => ({
      key: p.id, href: `/feed?q=${enc}#post-${p.id}`,
      node: (
        <>
          <span className="avatar sp-avatar" aria-hidden>{initial(p.author)}</span>
          <span className="sp-main">
            <span className="sp-title"><Hl text={p.author || "Fan"} q={term} /></span>
            <span className="sp-sub"><Hl text={p.body.replace(/\s+/g, " ").slice(0, 140) || "(no text)"} q={term} /></span>
          </span>
          <span className="sp-side">
            {p.likedBy.slice(0, 3).map((m) => <MemberBadge key={m} member={m} />)}
            <span className="muted small">{ago(p.createdAt)}</span>
          </span>
        </>
      ),
    }));
    if (tab === "fans") return res.fans.items.map((f: FanMatch) => ({
      key: f.nickname, href: `/feed?q=${encodeURIComponent(f.nickname)}`,
      node: (
        <>
          <span className="avatar sp-avatar" aria-hidden>{initial(f.nickname)}</span>
          <span className="sp-main">
            <span className="sp-title"><Hl text={f.nickname} q={term} /></span>
            <span className="sp-sub">{f.posts} post{f.posts === 1 ? "" : "s"} · last {ago(f.lastAt)}</span>
          </span>
        </>
      ),
    }));
    return res.activity.items.map((e: ArtistEvent) => ({
      key: e.id, href: `/feed?artist=1&q=${enc}`,
      node: (
        <>
          <MemberBadge member={e.member} />
          <span className="sp-main">
            <span className="sp-title">{e.kind === "like" ? "liked" : "commented on"} <Hl text={e.postAuthor || "a fan"} q={term} />&apos;s post</span>
            <span className="sp-sub"><Hl text={(e.kind === "comment" && e.commentBody) || e.postExcerpt || "(no text)"} q={term} /></span>
          </span>
          <span className="sp-side"><span className="muted small">{ago(e.at ?? e.detectedAt)}</span></span>
        </>
      ),
    }));
  }, [res, tab, term, enc]);

  const close = useCallback(() => setOpen(false), []);
  const go = useCallback((href: string) => { setOpen(false); router.push(href); }, [router]);

  const openButton = (
    <button type="button" className="sp-open" onClick={() => setOpen(true)} aria-label="Search (Ctrl K)">
      <SearchIcon /><span>Search</span><kbd>Ctrl K</kbd>
    </button>
  );
  if (!open) return openButton;

  const count = (t: SearchTab) => !res ? null : t === "posts" ? res.posts.total : t === "fans" ? res.fans.total : res.activity.total;
  const countLabel = (t: SearchTab) => { const c = count(t); return c === null ? "" : t === "activity" && res?.activity.more ? `${c}+` : String(c); };

  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === "Escape") { e.preventDefault(); close(); }
    else if (e.key === "ArrowDown") { e.preventDefault(); setActive((a) => Math.min(a + 1, Math.max(0, items.length - 1))); }
    else if (e.key === "ArrowUp") { e.preventDefault(); setActive((a) => Math.max(a - 1, 0)); }
    else if (e.key === "Enter" && term) { e.preventDefault(); go(items[active]?.href ?? allHref); }
    else if (e.key === "Tab" && res) { // Tab / Shift+Tab switch result tabs
      e.preventDefault();
      const i = TABS.findIndex((t) => t.id === tab);
      setTab(TABS[(i + (e.shiftKey ? TABS.length - 1 : 1)) % TABS.length].id); setActive(0);
    }
  };

  return (
    <>
    {openButton}
    <div className="sp-backdrop" onMouseDown={close}>
      <div className="sp" role="dialog" aria-modal="true" aria-label="Search" onMouseDown={(e) => e.stopPropagation()} onKeyDown={onKey}>
        <div className="sp-input-row">
          <SearchIcon />
          <input ref={input} className="sp-input" value={q} onChange={(e) => setQ(e.target.value)} placeholder="Search posts, fans or artist activity"
            autoComplete="off" spellCheck={false} aria-label="Search" aria-controls="sp-results" />
          {q && <button type="button" className="sp-clear" onClick={() => { setQ(""); input.current?.focus(); }} aria-label="Clear">×</button>}
          <button type="button" className="sp-esc" onClick={close}>Esc</button>
        </div>

        {term && (
          <div className="sp-tabs" role="tablist" aria-label="Result type">
            {TABS.map((t) => (
              <button key={t.id} type="button" role="tab" aria-selected={tab === t.id} className="sp-tab"
                onClick={() => { setTab(t.id); setActive(0); input.current?.focus(); }}>
                {t.label}{countLabel(t.id) && <span className="sp-count"> ({countLabel(t.id)})</span>}
              </button>
            ))}
          </div>
        )}

        <div className="sp-results" id="sp-results" role="listbox" aria-label="Results">
          {!term ? (
            <div className="sp-empty">Type to search post text, fan nicknames and artist likes or comments.
              <span className="muted"> ↑↓ to move · Enter to open · Tab to switch type</span></div>
          ) : error ? (
            <div className="sp-empty status warning">▲ Search failed ({error})</div>
          ) : !res || (loading && !items.length) ? (
            <div className="sp-empty muted">Searching…</div>
          ) : !items.length ? (
            <div className="sp-empty muted">No {TABS.find((t) => t.id === tab)!.label.toLowerCase()} match &ldquo;{term}&rdquo;.</div>
          ) : items.map((it, i) => (
            <button key={it.key} type="button" role="option" aria-selected={i === active} className="sp-item"
              onMouseEnter={() => setActive(i)} onClick={() => go(it.href)}>
              {it.node}
            </button>
          ))}
        </div>

        {term && res && (count(tab) ?? 0) > 0 && (
          <button type="button" className="sp-all" onClick={() => go(allHref)}>See all results</button>
        )}
      </div>
    </div>
    </>
  );
}
