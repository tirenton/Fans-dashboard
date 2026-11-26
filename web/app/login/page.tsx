import Link from "next/link";
import { getSession } from "@/lib/auth";
import { signupOpen } from "@/lib/accounts";
import { safeNext } from "@/lib/session";

export const dynamic = "force-dynamic";

type Search = Promise<{ next?: string; error?: string; need?: string; confirmed?: string; reset?: string }>;

const ERRORS: Record<string, string> = {
  bad: "Wrong name, email or password.",
  locked: "Too many failed tries. Wait 15 minutes and try again.",
  down: "Login isn't available right now. Try again in a minute.",
  setup: "Login isn't set up on this server yet (SESSION_SECRET is missing).",
};

export default async function LoginPage({ searchParams }: { searchParams: Search }) {
  const [sp, s, open] = await Promise.all([searchParams, getSession(), signupOpen()]);
  const next = safeNext(sp.next);
  const error = sp.error ? ERRORS[sp.error] ?? ERRORS.bad : undefined;
  const confirmed = sp.confirmed && /^[a-z0-9_.-]{1,32}$/.test(sp.confirmed) ? sp.confirmed : undefined;
  return (
    <div className="login">
      <h1>Log in</h1>
      {sp.need === "admin" && s ? (
        <p className="lede">That page needs an admin account. You&apos;re signed in as <b>{s.name}</b>; log in as an admin to open it.</p>
      ) : s ? (
        <p className="lede">You&apos;re signed in as <b>{s.name}</b>. <Link href={next}>Continue</Link></p>
      ) : (
        <p className="lede">The Main dashboard is open to everyone. Log in to join its chat and send feedback.</p>
      )}
      <form className="card login-form" method="post" action="/api/auth/login">
        {confirmed && <p className="status good" role="status">Email confirmed. Your account {confirmed} is ready: log in.</p>}
        {sp.reset && <p className="status good" role="status">Password changed. Log in with your new password.</p>}
        <input type="hidden" name="next" value={next} />
        <label>
          <span>Name or email</span>
          <input name="name" autoComplete="username" required maxLength={254} autoCapitalize="none" spellCheck={false}
            defaultValue={confirmed ?? ""} autoFocus={!confirmed} />
        </label>
        <label>
          <span>Password</span>
          <input name="password" type="password" autoComplete="current-password" required maxLength={1024} autoFocus={!!confirmed} />
        </label>
        {error && <p className="status critical" role="alert">{error}</p>}
        <button type="submit" className="login-submit">Log in</button>
        {open && <p className="muted small login-links"><Link href="/forgot">Forgot password?</Link></p>}
      </form>
      <p className="muted small">
        {open && <>No account yet? <Link href="/signup">Sign up</Link> · </>}
        <Link href="/">← Main dashboard</Link>
      </p>
    </div>
  );
}
