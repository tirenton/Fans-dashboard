"use client";

// The Main dashboard's open chat and feedback box. Signed-in people of any role can chat and send feedback;
// visitors see a log-in card instead. Admins can remove chat messages and read feedback on the Admin page.
// Messages come in bubbles: yours on the right, others on the left with their picture and name, one name per run
// of messages, and a line for each new day.
import Link from "next/link";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Avatar, shownName, type ProfileInfo } from "@/components/avatar";
import { LocalTime } from "@/components/client";
import { armMessageSound, playMessageSound, setSoundPref, soundPref } from "@/components/chat-sound";

type Me = { name: string; role: "admin" | "user"; profile?: ProfileInfo } | null;
type Message = { id: number; name: string; admin?: boolean; text: string; at: string };
type View = { version: number; messages: Message[]; profiles?: Record<string, ProfileInfo> };
type Feedback = { id: number; name: string; text: string; at: string };

const CHAT_MAX = 500;
const FEEDBACK_MAX = 2000;
const GROUP_GAP = 5 * 60_000; // a pause this long starts a new run of bubbles

async function errorOf(res: Response) {
  const body = await res.json().catch(() => null);
  if (res.status === 429) return "You're sending too fast. Wait a few seconds.";
  if (res.status === 401) return "Your login has ended. Log in again.";
  return String(body?.error ?? `Something went wrong (${res.status}).`);
}

const visible = () =>
  new Promise<void>((resolve) => {
    if (!document.hidden) return resolve();
    const on = () => {
      if (!document.hidden) {
        document.removeEventListener("visibilitychange", on);
        resolve();
      }
    };
    document.addEventListener("visibilitychange", on);
  });

const dayKey = (d: Date) => `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
function dayLabel(d: Date) {
  const today = new Date();
  const yesterday = new Date(today.getFullYear(), today.getMonth(), today.getDate() - 1);
  if (dayKey(d) === dayKey(today)) return "Today";
  if (dayKey(d) === dayKey(yesterday)) return "Yesterday";
  return d.toLocaleDateString(undefined, { weekday: "short", day: "numeric", month: "short", year: d.getFullYear() === today.getFullYear() ? undefined : "numeric" });
}
const timeOf = (d: Date) => d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });

type Run = { day?: string; name: string; admin: boolean; msgs: Message[] };

/** Consecutive messages from one person within a few minutes form a run; a new day gets a divider label. */
function runsOf(messages: Message[]): Run[] {
  const runs: Run[] = [];
  let lastDay = "";
  for (const m of messages) {
    const d = new Date(m.at);
    const k = dayKey(d);
    const prev = runs[runs.length - 1];
    const last = prev?.msgs[prev.msgs.length - 1];
    if (k !== lastDay) {
      runs.push({ day: dayLabel(d), name: m.name, admin: !!m.admin, msgs: [m] });
      lastDay = k;
    } else if (prev && prev.name === m.name && d.getTime() - new Date(last!.at).getTime() < GROUP_GAP) {
      prev.msgs.push(m);
    } else {
      runs.push({ name: m.name, admin: !!m.admin, msgs: [m] });
    }
  }
  return runs;
}

function SendIcon() {
  return (
    <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden>
      <path d="M3.4 20.4 21 12 3.4 3.6l-.01 6.53L15 12 3.39 13.87z" fill="currentColor" />
    </svg>
  );
}

export function Chat({ me, signupOpen = false }: { me: Me; signupOpen?: boolean }) {
  const [view, setView] = useState<View | null>(null);
  const [offline, setOffline] = useState(false);
  const [text, setText] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [sending, setSending] = useState(false);
  const [unseen, setUnseen] = useState(0);
  const log = useRef<HTMLDivElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const stick = useRef(true); // follow new messages while the reader is at the bottom
  const lastId = useRef(0);
  // new-message sound (chat-sound.ts): on unless muted in this browser
  const [soundOn, setSoundOn] = useState(true);
  const soundOnRef = useRef(true);
  const heardId = useRef<number | null>(null); // newest message already accounted for; null until the first load
  useEffect(() => {
    const on = soundPref();
    setSoundOn(on);
    soundOnRef.current = on;
    const once = armMessageSound();
    return () => {
      window.removeEventListener("pointerdown", once);
      window.removeEventListener("keydown", once);
    };
  }, []);
  const toggleSound = () => {
    const on = !soundOn;
    setSoundOn(on);
    soundOnRef.current = on;
    setSoundPref(on);
    if (on) playMessageSound(); // a sample, so you know what it sounds like (the click allows sound)
  };

  // wait for changes: the server holds each request until the chat changes (or 25 s pass).
  // Keyed on the name, not `me`: the page's 60 s refresh hands over a new `me` object each time.
  const signedIn = me?.name;
  useEffect(() => {
    if (!signedIn) return;
    const ctl = new AbortController();
    let version: number | null = null;
    let delay = 2000;
    (async () => {
      while (!ctl.signal.aborted) {
        await visible();
        try {
          const res = await fetch(`/api/chat${version === null ? "" : `?v=${version}`}`, { signal: ctl.signal, cache: "no-store" });
          if (res.status === 401 || res.status === 403) {
            setOffline(true);
            return;
          }
          if (!res.ok) throw new Error(String(res.status));
          const v = (await res.json()) as View;
          version = v.version;
          setView(v);
          setOffline(false);
          delay = 2000;
        } catch {
          if (ctl.signal.aborted) return;
          setOffline(true);
          await new Promise((r) => setTimeout(r, delay));
          delay = Math.min(delay * 2, 30_000);
        }
      }
    })();
    return () => ctl.abort();
  }, [signedIn]);

  // follow the conversation, or count what arrived while the reader scrolled up
  useEffect(() => {
    const el = log.current;
    const newest = view?.messages[view.messages.length - 1]?.id ?? 0;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
    else if (newest > lastId.current && lastId.current > 0) {
      const seen = lastId.current; // read now: the updater below runs after lastId moves on
      const fresh = view!.messages.filter((m) => m.id > seen && m.name !== me?.name).length;
      if (fresh) setUnseen((n) => n + fresh);
    }
    lastId.current = Math.max(lastId.current, newest);
  }, [view, me?.name]);

  // one sound per batch of new messages from other people; none for what was already there when the page opened
  useEffect(() => {
    const msgs = view?.messages;
    if (!msgs) return;
    const newest = msgs[msgs.length - 1]?.id ?? 0;
    const since = heardId.current;
    heardId.current = Math.max(since ?? 0, newest);
    if (since === null) return;
    if (soundOnRef.current && msgs.some((m) => m.id > since && m.name !== me?.name)) playMessageSound();
  }, [view, me?.name]);

  const toBottom = () => {
    const el = log.current;
    if (el) el.scrollTo({ top: el.scrollHeight, behavior: "smooth" });
    stick.current = true;
    setUnseen(0);
  };

  const send = useCallback(async () => {
    const t = text.trim();
    if (!t || sending) return;
    setSending(true);
    setError(null);
    try {
      const res = await fetch("/api/chat", {
        method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ text: t }),
      });
      if (!res.ok) {
        setError(await errorOf(res));
        return;
      }
      const m = (await res.json()) as Message;
      stick.current = true;
      setUnseen(0);
      setText("");
      // show it straight away; the next wait brings the same list from the server
      setView((v) => (v && !v.messages.some((x) => x.id === m.id) ? { ...v, messages: [...v.messages, m] } : v));
    } catch {
      setError("Couldn't reach the server. Try again.");
    } finally {
      setSending(false);
      input.current?.focus();
    }
  }, [text, sending]);

  const remove = async (m: Message) => {
    if (!confirm(`Remove this message from ${m.name}?\n\n${m.text}`)) return;
    const res = await fetch(`/api/chat/${m.id}`, { method: "DELETE" });
    if (!res.ok && res.status !== 404) setError(await errorOf(res));
    else setView((v) => (v ? { ...v, messages: v.messages.filter((x) => x.id !== m.id) } : v));
  };

  const removePicture = async (name: string) => {
    if (!confirm(`Remove ${name}'s profile picture?`)) return;
    const res = await fetch(`/api/profile/avatar?name=${encodeURIComponent(name)}`, { method: "DELETE" });
    if (!res.ok) setError(await errorOf(res));
  };

  const runs = useMemo(() => runsOf(view?.messages ?? []), [view]);
  const profiles = view?.profiles ?? {};
  const isAdmin = me?.role === "admin";
  const left = CHAT_MAX - text.length;

  return (
    <section className="card section chat" aria-labelledby="chat-title">
      <div className="chat-head">
        <h2 id="chat-title">Chat</h2>
        {me && (offline
          ? <span className="chat-live off" role="status">Reconnecting…</span>
          : view && <span className="chat-live" role="status">Live</span>)}
        {me && (
          <button type="button" className="chat-sound" aria-pressed={soundOn} onClick={toggleSound}
            title={soundOn ? "Sound on for new messages. Click to mute." : "Sound off. Click to play a sound for new messages."}
            aria-label={soundOn ? "Mute new-message sound" : "Turn on new-message sound"}>
            <span aria-hidden>{soundOn ? "🔔" : "🔕"}</span>
          </button>
        )}
        {me && (
          <Link href="/profile" className="chat-me" title="Change your picture and name">
            <Avatar name={me.name} profile={profiles[me.name] ?? me.profile} size={22} />
            <span className="me-name">Edit profile</span>
          </Link>
        )}
      </div>
      {!me ? (
        <div className="chat-locked">
          <div className="chat-locked-icon" aria-hidden>💬</div>
          <p><b>Join the conversation</b></p>
          <p className="muted small">Log in to read the chat and talk with other fans.</p>
          <div className="row-actions">
            <Link className="btn-primary" href="/login">Log in</Link>
            {signupOpen && <Link className="chip" href="/signup">Create an account</Link>}
          </div>
        </div>
      ) : (
        <>
          <div className="chat-log-wrap">
            <div
              className="chat-log" ref={log} role="log" aria-live="polite" aria-label="Chat messages"
              onScroll={(e) => {
                const el = e.currentTarget;
                stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
                if (stick.current) setUnseen(0);
              }}
            >
              {view === null ? (
                <p className="chat-empty muted small">Loading…</p>
              ) : view.messages.length === 0 ? (
                <div className="chat-empty">
                  <div className="chat-locked-icon" aria-hidden>🐳</div>
                  <p className="muted">No messages yet. Say hi!</p>
                </div>
              ) : (
                runs.map((r) => {
                  const mine = r.name === me.name;
                  const p = profiles[r.name];
                  const lastAt = new Date(r.msgs[r.msgs.length - 1].at);
                  return (
                    <div key={r.msgs[0].id} className="chat-run-wrap">
                      {r.day && <div className="chat-day"><span>{r.day}</span></div>}
                      <div className={`chat-run${mine ? " mine" : ""}`}>
                        {!mine && (
                          <div className="chat-run-avatar">
                            <Avatar name={r.name} profile={p} size={34} />
                            {isAdmin && p?.avatar ? (
                              <button type="button" className="chat-pic-del" onClick={() => removePicture(r.name)} title="Remove this picture" aria-label={`Remove ${r.name}'s picture`}>×</button>
                            ) : null}
                          </div>
                        )}
                        <div className="chat-stack">
                          {!mine && (
                            <div className="chat-who">
                              <b>{shownName(r.name, p)}</b>
                              {p?.display && <span className="muted">@{r.name}</span>}
                              {r.admin && <span className="chat-admin">admin</span>}
                            </div>
                          )}
                          {r.msgs.map((m) => (
                            <div key={m.id} className="chat-bubble" title={timeOf(new Date(m.at))}>
                              <span className="chat-text">{m.text}</span>
                              {isAdmin && (
                                <button type="button" className="chat-del" onClick={() => remove(m)} aria-label={`Remove message from ${m.name}`} title="Remove">×</button>
                              )}
                            </div>
                          ))}
                          <time className="chat-time" dateTime={lastAt.toISOString()}>{timeOf(lastAt)}</time>
                        </div>
                      </div>
                    </div>
                  );
                })
              )}
            </div>
            {unseen > 0 && (
              <button type="button" className="chat-jump" onClick={toBottom}>
                {unseen} new message{unseen === 1 ? "" : "s"} ↓
              </button>
            )}
          </div>
          <form className="chat-form" onSubmit={(e) => { e.preventDefault(); send(); }}>
            <Avatar name={me.name} profile={profiles[me.name] ?? me.profile} size={32} className="chat-form-avatar" />
            <div className="chat-pill">
              <input
                ref={input} className="chat-input" value={text} onChange={(e) => setText(e.target.value)} maxLength={CHAT_MAX}
                placeholder="Write a message…" aria-label="Message" autoComplete="off" enterKeyHint="send"
              />
              {left <= 50 && <span className={`chat-left${left <= 10 ? " low" : ""}`} aria-live="polite">{left}</span>}
              <button type="submit" className="chat-send" disabled={sending || !text.trim()} aria-label="Send" title="Send">
                <SendIcon />
              </button>
            </div>
          </form>
          {error && <p className="status critical chat-error" role="alert">{error}</p>}
        </>
      )}
    </section>
  );
}

export function FeedbackForm({ me }: { me: Me }) {
  const [text, setText] = useState("");
  const [state, setState] = useState<{ kind: "sent" | "error"; msg: string } | null>(null);
  const [sending, setSending] = useState(false);

  const send = async () => {
    if (!text.trim() || sending) return;
    setSending(true);
    setState(null);
    try {
      const res = await fetch("/api/feedback", {
        method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ text }),
      });
      if (!res.ok) {
        setState({ kind: "error", msg: res.status === 429 ? "You've sent a lot of feedback recently. Try again in an hour." : await errorOf(res) });
        return;
      }
      setText("");
      setState({ kind: "sent", msg: "Thanks! Your feedback went to the admins." });
    } catch {
      setState({ kind: "error", msg: "Couldn't reach the server. Try again." });
    } finally {
      setSending(false);
    }
  };

  return (
    <section className="card section feedback" aria-labelledby="feedback-title">
      <h2 id="feedback-title">Feedback</h2>
      {!me ? (
        <p className="muted"><Link href="/login">Log in</Link> to send feedback about this dashboard.</p>
      ) : (
        <form className="feedback-form" onSubmit={(e) => { e.preventDefault(); send(); }}>
          <p className="muted small">Ideas, problems or anything else about this dashboard. Only the admins see it.</p>
          <textarea
            value={text} onChange={(e) => setText(e.target.value)} maxLength={FEEDBACK_MAX} rows={4}
            aria-label="Your feedback" placeholder="Your feedback"
          />
          <div className="feedback-row">
            <span className="muted small">{text.length > 0 ? `${FEEDBACK_MAX - text.length} characters left` : ""}</span>
            <button type="submit" className="btn-primary" disabled={sending || !text.trim()}>Send feedback</button>
          </div>
          {state && <p className={`status ${state.kind === "sent" ? "good" : "critical"}`} role="status">{state.msg}</p>}
        </form>
      )}
    </section>
  );
}

/** Admin page: what people sent, newest first, with Done to clear an item. */
export function FeedbackList() {
  const [items, setItems] = useState<Feedback[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const res = await fetch("/api/feedback", { cache: "no-store" });
      if (!res.ok) throw new Error(await errorOf(res));
      setItems(await res.json());
      setError(null);
    } catch (e) {
      setError(String((e as Error).message));
    }
  }, []);
  useEffect(() => {
    load();
  }, [load]);

  const done = async (f: Feedback) => {
    const res = await fetch(`/api/feedback/${f.id}`, { method: "DELETE" });
    if (!res.ok && res.status !== 404) return setError(await errorOf(res));
    setItems((xs) => xs?.filter((x) => x.id !== f.id) ?? null);
  };

  return (
    <section className="card section" aria-labelledby="fb-list-title">
      <div className="chat-head">
        <h2 id="fb-list-title">Feedback{items && items.length > 0 ? ` (${items.length})` : ""}</h2>
        <button type="button" className="chip" onClick={load}>Refresh</button>
      </div>
      {error && <p className="status critical" role="alert">{error}</p>}
      {items === null ? (
        !error && <p className="muted small">Loading…</p>
      ) : items.length === 0 ? (
        <p className="muted small">No feedback right now. People send it from the bottom of the Main dashboard.</p>
      ) : (
        <div className="stack">
          {items.map((f) => (
            <div key={f.id} className="fb-item">
              <div className="chat-meta">
                <b>{f.name}</b>
                <span className="muted"><LocalTime iso={f.at} /></span>
                <button type="button" className="chip fb-done" onClick={() => done(f)}>Done</button>
              </div>
              <div className="chat-text">{f.text}</div>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}
