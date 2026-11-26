import Link from "next/link";
import { ACCOUNT_ERRORS } from "@/lib/accounts";

export const dynamic = "force-dynamic";
export const metadata = { title: "New password · AxisMixx" };

// The link in the reset mail opens this page.
export default async function ResetPage({ searchParams }: { searchParams: Promise<{ t?: string; error?: string }> }) {
  const sp = await searchParams;
  const t = /^[A-Za-z0-9_-]{43}$/.test(sp.t ?? "") ? sp.t! : "";
  const error = sp.error ? ACCOUNT_ERRORS[sp.error] ?? ACCOUNT_ERRORS.down : undefined;
  return (
    <div className="login">
      <h1>Choose a new password</h1>
      {!t ? (
        <div className="card login-form">
          <p className="status critical" role="alert">{ACCOUNT_ERRORS.token}</p>
          <p className="muted small">Reset links work for 1 hour, once. <Link href="/forgot">Get a new one</Link>.</p>
        </div>
      ) : (
        <form className="card login-form" method="post" action="/api/auth/reset">
          <input type="hidden" name="t" value={t} />
          <label>
            <span>New password <span className="muted small">(at least 10 characters)</span></span>
            <input name="password" type="password" autoComplete="new-password" required minLength={10} maxLength={1024} autoFocus />
          </label>
          <label>
            <span>New password again</span>
            <input name="password2" type="password" autoComplete="new-password" required minLength={10} maxLength={1024} />
          </label>
          {error && <p className="status critical" role="alert">{error}</p>}
          <button type="submit" className="login-submit">Save new password</button>
        </form>
      )}
      <p className="muted small"><Link href="/login">← Log in</Link></p>
    </div>
  );
}
