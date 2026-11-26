import Link from "next/link";
import Script from "next/script";
import { getSession } from "@/lib/auth";
import { ACCOUNT_ERRORS, signupOpen, turnstileSiteKey } from "@/lib/accounts";

export const dynamic = "force-dynamic";
export const metadata = { title: "Sign up · AxisMixx" };

type Search = Promise<{ sent?: string; error?: string; name?: string }>;

export default async function SignupPage({ searchParams }: { searchParams: Search }) {
  const [sp, s, open] = await Promise.all([searchParams, getSession(), signupOpen()]);
  const siteKey = turnstileSiteKey();
  const error = sp.error ? ACCOUNT_ERRORS[sp.error] ?? ACCOUNT_ERRORS.down : undefined;
  return (
    <div className="login">
      <h1>Sign up</h1>
      {s ? (
        <p className="lede">You&apos;re already signed in as <b>{s.name}</b>. <Link href="/">Go to the Main dashboard</Link></p>
      ) : !open ? (
        <p className="lede">Sign-up isn&apos;t open on this server yet. Ask an admin for an account.</p>
      ) : sp.sent ? (
        <div className="card login-form">
          <p className="status good" role="status">Check your email</p>
          <p>We sent you a link to confirm your address. Open it within 24 hours to finish creating your account.</p>
          <p className="muted small">Nothing there after a few minutes? Look in your spam folder, or sign up again.</p>
        </div>
      ) : (
        <>
          <p className="lede">Create an account to join the chat and send feedback. We&apos;ll email you a link to confirm your address.</p>
          <form className="card login-form" method="post" action="/api/auth/signup">
            <label>
              <span>Name <span className="muted small">(shown in the chat)</span></span>
              <input name="name" autoComplete="username" required minLength={3} maxLength={32} pattern="[A-Za-z0-9][A-Za-z0-9_.\-]{2,31}"
                title="3–32 characters: letters, digits, dot, dash or underscore" autoCapitalize="none" spellCheck={false}
                defaultValue={sp.name ?? ""} autoFocus />
            </label>
            <label>
              <span>Email</span>
              <input name="email" type="email" autoComplete="email" required maxLength={254} spellCheck={false} />
            </label>
            <label>
              <span>Password <span className="muted small">(at least 10 characters)</span></span>
              <input name="password" type="password" autoComplete="new-password" required minLength={10} maxLength={1024} />
            </label>
            <label>
              <span>Password again</span>
              <input name="password2" type="password" autoComplete="new-password" required minLength={10} maxLength={1024} />
            </label>
            {siteKey && (
              <>
                <Script src="https://challenges.cloudflare.com/turnstile/v0/api.js" strategy="afterInteractive" />
                <div className="cf-turnstile" data-sitekey={siteKey} data-theme="dark" />
              </>
            )}
            {error && <p className="status critical" role="alert">{error}</p>}
            <button type="submit" className="login-submit">Sign up</button>
          </form>
        </>
      )}
      <p className="muted small">Already have an account? <Link href="/login">Log in</Link> · <Link href="/">← Main dashboard</Link></p>
    </div>
  );
}
