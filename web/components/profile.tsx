"use client";

// The profile page: change your picture (pick, drag to place, zoom, save), your display name and your password.
// The picture is cropped and shrunk here to a 256×256 JPEG; the server re-draws it again before keeping it.
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from "react";
import { Avatar, shownName, type ProfileInfo } from "@/components/avatar";

export type Me = ProfileInfo & { name: string; email?: string; canChangePassword: boolean };

const ERRORS: Record<string, string> = {
  toolong: "Display names are up to 32 characters.",
  image: "That file isn't a picture this site can use. Pick a JPEG or PNG photo.",
  fast: "Too many changes in a short time. Wait a minute and try again.",
  current: "Your current password isn't right.",
  password: "New passwords need at least 10 characters.",
  pc: "This PC copies its accounts from the website. Change your password on axismixx.com instead.",
};

async function errorOf(res: Response) {
  if (res.status === 401) return "Your login has ended. Log in again.";
  const body = await res.json().catch(() => null);
  return ERRORS[body?.error] ?? "That didn't work. Try again in a minute.";
}

type Note = { kind: "good" | "critical"; msg: string } | null;
const Status = ({ note }: { note: Note }) =>
  note && <p className={`status ${note.kind}`} role={note.kind === "critical" ? "alert" : "status"}>{note.msg}</p>;

const VIEW = 240; // the crop box on screen
const OUT = 256; // the picture that is uploaded

/** Pick a photo, drag it into place and zoom; Save uploads the square. */
function PictureCropper({ file, onDone, onCancel }: { file: File; onDone: (blob: Blob) => void; onCancel: () => void }) {
  const canvas = useRef<HTMLCanvasElement>(null);
  const [img, setImg] = useState<HTMLImageElement | null>(null);
  const [failed, setFailed] = useState(false);
  const [zoom, setZoom] = useState(1);
  const [off, setOff] = useState({ x: 0, y: 0 });
  const drag = useRef<{ x: number; y: number; ox: number; oy: number } | null>(null);

  useEffect(() => {
    const url = URL.createObjectURL(file);
    const i = new Image();
    i.onload = () => setImg(i);
    i.onerror = () => setFailed(true);
    i.src = url;
    return () => URL.revokeObjectURL(url);
  }, [file]);

  // the image covers the square at zoom 1; offsets are kept so no empty edge shows
  const geometry = (side: number, o = off, z = zoom) => {
    if (!img) return null;
    const scale = (side / Math.min(img.width, img.height)) * z;
    const w = img.width * scale, h = img.height * scale;
    const k = side / VIEW;
    const x = Math.min(0, Math.max(side - w, (side - w) / 2 + o.x * k));
    const y = Math.min(0, Math.max(side - h, (side - h) / 2 + o.y * k));
    return { x, y, w, h };
  };

  useEffect(() => {
    const c = canvas.current, g = geometry(VIEW * 2);
    if (!c || !g || !img) return;
    const ctx = c.getContext("2d")!;
    ctx.clearRect(0, 0, c.width, c.height);
    ctx.drawImage(img, g.x, g.y, g.w, g.h);
  });

  const clampOff = (o: { x: number; y: number }, z = zoom) => {
    const g = geometry(VIEW, o, z);
    if (!g) return o;
    return { x: g.x - (VIEW - g.w) / 2, y: g.y - (VIEW - g.h) / 2 };
  };

  const down = (e: ReactPointerEvent) => {
    e.currentTarget.setPointerCapture(e.pointerId);
    drag.current = { x: e.clientX, y: e.clientY, ox: off.x, oy: off.y };
  };
  const move = (e: ReactPointerEvent) => {
    const d = drag.current;
    if (d) setOff(clampOff({ x: d.ox + e.clientX - d.x, y: d.oy + e.clientY - d.y }));
  };

  const save = () => {
    const g = geometry(OUT);
    if (!img || !g) return;
    const c = document.createElement("canvas");
    c.width = c.height = OUT;
    const ctx = c.getContext("2d")!;
    ctx.fillStyle = "#161618";
    ctx.fillRect(0, 0, OUT, OUT);
    ctx.imageSmoothingQuality = "high";
    ctx.drawImage(img, g.x, g.y, g.w, g.h);
    c.toBlob((b) => b && onDone(b), "image/jpeg", 0.9);
  };

  if (failed) {
    return (
      <div className="cropper">
        <p className="status critical" role="alert">This browser can&apos;t open that file. Pick a JPEG or PNG photo.</p>
        <button type="button" className="chip" onClick={onCancel}>Back</button>
      </div>
    );
  }
  return (
    <div className="cropper">
      <canvas
        ref={canvas} width={VIEW * 2} height={VIEW * 2} className="crop-box" aria-label="Drag to place your picture"
        onPointerDown={down} onPointerMove={move} onPointerUp={() => (drag.current = null)} onPointerCancel={() => (drag.current = null)}
      />
      <label className="crop-zoom">
        <span className="muted small">Zoom</span>
        <input
          type="range" min={1} max={4} step={0.01} value={zoom}
          onChange={(e) => {
            const z = Number(e.target.value);
            setZoom(z);
            setOff((o) => clampOff(o, z));
          }}
        />
      </label>
      <p className="muted small">Drag the picture to place it in the circle.</p>
      <div className="row-actions">
        <button type="button" className="btn-primary" onClick={save} disabled={!img}>Save picture</button>
        <button type="button" className="chip" onClick={onCancel}>Cancel</button>
      </div>
    </div>
  );
}

export function ProfileEditor({ me, role }: { me: Me; role: "admin" | "user" }) {
  const router = useRouter();
  const [profile, setProfile] = useState<ProfileInfo>({ display: me.display, avatar: me.avatar });
  const [file, setFile] = useState<File | null>(null);
  const [picNote, setPicNote] = useState<Note>(null);
  const [busy, setBusy] = useState(false);
  const [display, setDisplay] = useState(me.display ?? "");
  const [nameNote, setNameNote] = useState<Note>(null);
  const [pw, setPw] = useState({ current: "", next: "", again: "" });
  const [pwNote, setPwNote] = useState<Note>(null);
  const picker = useRef<HTMLInputElement>(null);

  const upload = async (blob: Blob) => {
    setBusy(true);
    setPicNote(null);
    try {
      const res = await fetch("/api/profile/avatar", { method: "PUT", headers: { "Content-Type": "image/jpeg" }, body: blob });
      if (!res.ok) return setPicNote({ kind: "critical", msg: await errorOf(res) });
      setProfile(await res.json());
      setFile(null);
      setPicNote({ kind: "good", msg: "Picture saved." });
      router.refresh();
    } catch {
      setPicNote({ kind: "critical", msg: "Couldn't reach the server. Try again." });
    } finally {
      setBusy(false);
    }
  };

  const removePicture = async () => {
    if (!confirm("Remove your picture?")) return;
    const res = await fetch("/api/profile/avatar", { method: "DELETE" });
    if (!res.ok) return setPicNote({ kind: "critical", msg: await errorOf(res) });
    setProfile(await res.json());
    setPicNote({ kind: "good", msg: "Picture removed." });
    router.refresh();
  };

  const saveName = async () => {
    setNameNote(null);
    const res = await fetch("/api/profile", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ display }) });
    if (!res.ok) return setNameNote({ kind: "critical", msg: await errorOf(res) });
    const p = (await res.json()) as ProfileInfo;
    setProfile(p);
    setDisplay(p.display ?? "");
    setNameNote({ kind: "good", msg: p.display ? "Name saved." : "Name cleared: the chat shows your account name." });
    router.refresh();
  };

  const savePassword = async () => {
    setPwNote(null);
    if (pw.next !== pw.again) return setPwNote({ kind: "critical", msg: "The two new passwords are different." });
    const res = await fetch("/api/profile/password", {
      method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ current: pw.current, new: pw.next }),
    });
    if (!res.ok) return setPwNote({ kind: "critical", msg: await errorOf(res) });
    setPw({ current: "", next: "", again: "" });
    setPwNote({ kind: "good", msg: "Password changed. Use the new one next time you log in." });
  };

  return (
    <div className="profile-grid">
      <section className="card profile-hero" aria-label="Your picture">
        {file ? (
          <PictureCropper file={file} onDone={upload} onCancel={() => setFile(null)} />
        ) : (
          <>
            <Avatar name={me.name} profile={profile} size={112} className="profile-avatar" />
            <div className="profile-who">
              <div className="profile-name">{shownName(me.name, profile)}</div>
              <div className="muted">{profile.display ? `@${me.name}` : "No display name yet"}{role === "admin" && <span className="chat-admin">admin</span>}</div>
            </div>
            <div className="row-actions">
              <button type="button" className="btn-primary" onClick={() => picker.current?.click()} disabled={busy}>
                {profile.avatar ? "Change picture" : "Add a picture"}
              </button>
              {profile.avatar ? <button type="button" className="chip" onClick={removePicture}>Remove</button> : null}
            </div>
          </>
        )}
        <input
          ref={picker} type="file" accept="image/jpeg,image/png,image/webp,image/gif,image/heic" hidden
          onChange={(e) => {
            const f = e.target.files?.[0];
            e.target.value = "";
            if (!f) return;
            if (f.size > 25 * 1024 * 1024) return setPicNote({ kind: "critical", msg: "That file is too big (over 25 MB)." });
            setPicNote(null);
            setFile(f);
          }}
        />
        {busy && <p className="muted small">Saving…</p>}
        <Status note={picNote} />
      </section>

      <section className="card profile-section" aria-labelledby="name-title">
        <h2 id="name-title">Display name</h2>
        <form className="login-form flat" onSubmit={(e) => { e.preventDefault(); saveName(); }}>
          <label>
            Shown in the chat instead of your account name (up to 32 characters, emoji welcome)
            <input value={display} onChange={(e) => setDisplay(e.target.value)} maxLength={32} placeholder={me.name} />
          </label>
          <p className="muted small">People still see @{me.name} next to it, so nobody can pretend to be someone else.</p>
          <div className="row-actions">
            <button type="submit" className="btn-primary">Save name</button>
          </div>
          <Status note={nameNote} />
        </form>
      </section>

      <section className="card profile-section" aria-labelledby="pw-title">
        <h2 id="pw-title">Password</h2>
        {me.email && <p className="muted small">Your account&apos;s email: <b>{me.email}</b></p>}
        {me.canChangePassword ? (
          <form className="login-form flat" onSubmit={(e) => { e.preventDefault(); savePassword(); }}>
            <label>
              Current password
              <input type="password" autoComplete="current-password" value={pw.current} onChange={(e) => setPw({ ...pw, current: e.target.value })} required />
            </label>
            <label>
              New password (at least 10 characters)
              <input type="password" autoComplete="new-password" minLength={10} value={pw.next} onChange={(e) => setPw({ ...pw, next: e.target.value })} required />
            </label>
            <label>
              New password again
              <input type="password" autoComplete="new-password" minLength={10} value={pw.again} onChange={(e) => setPw({ ...pw, again: e.target.value })} required />
            </label>
            <div className="row-actions">
              <button type="submit" className="btn-primary">Change password</button>
            </div>
            <Status note={pwNote} />
          </form>
        ) : (
          <p className="muted">This PC copies its accounts from the website, so change your password on axismixx.com.</p>
        )}
      </section>
    </div>
  );
}
