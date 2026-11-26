import Link from "next/link";
import { ACCOUNT_ERRORS, signupOpen } from "@/lib/accounts";

export const dynamic = "force-dynamic";
export const metadata = { title: "Forgot password · AxisMixx" };

export default async function ForgotPage({ searchParams }: { searchParams: Promise<{ sent?: string; error?: string }> }) {
  const [sp, open] = await Promise.all([searchParams, signupOpen()]);
  const error = sp.error ? ACCOUNT_ERRORS[sp.error] ?? ACCOUNT_ERRORS.down : undefined;
  return (
    <div className="login">
      <h1>Forgot password</h1>
      {!open ? (
        <p className="lede">Password reset by email isn&apos;t set up on this server. Ask an admin to set a new password for you.</p>
      ) : sp.sent ? (
        <div className="card login-form">
          <p className="status good" role="status">Check your email</p>
          <p>If that address belongs to an account, we sent it a link to choose a new password. It works for 1 hour.</p>
          <p className="muted small">Nothing there after a few minutes? Look in your spam folder.</p>
        </div>
      ) : (
        <>
          <p className="lede">Enter the email address of your account, and we&apos;ll send you a link to choose a new password.</p>
          <form className="card login-form" method="post" action="/api/auth/forgot">
            <label>
              <span>Email</span>
              <input name="email" type="email" autoComplete="email" required maxLength={254} spellCheck={false} autoFocus />
            </label>
            {error && <p className="status critical" role="alert">{error}</p>}
            <button type="submit" className="login-submit">Send reset link</button>
          </form>
        </>
      )}
      <p className="muted small"><Link href="/login">← Log in</Link></p>
    </div>
  );
}
