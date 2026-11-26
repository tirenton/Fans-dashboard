import Link from "next/link";
import { ACCOUNT_ERRORS } from "@/lib/accounts";

export const dynamic = "force-dynamic";
export const metadata = { title: "Confirm email · AxisMixx" };

// The link in the sign-up mail opens this page. It takes a click to confirm, so mail scanners that open links
// on their own don't confirm anything.
export default async function ConfirmPage({ searchParams }: { searchParams: Promise<{ t?: string; error?: string }> }) {
  const sp = await searchParams;
  const t = /^[A-Za-z0-9_-]{43}$/.test(sp.t ?? "") ? sp.t! : "";
  const error = sp.error ? ACCOUNT_ERRORS[sp.error] ?? ACCOUNT_ERRORS.down : !t ? ACCOUNT_ERRORS.token : undefined;
  return (
    <div className="login">
      <h1>Confirm your email</h1>
      {error ? (
        <div className="card login-form">
          <p className="status critical" role="alert">{error}</p>
          <p className="muted small">
            {sp.error === "taken" || sp.error === "emailtaken"
              ? <>Someone got there first. <Link href="/signup">Sign up again</Link> with another name, or <Link href="/forgot">reset your password</Link>.</>
              : <>Confirm links work for 24 hours. <Link href="/signup">Sign up again</Link> to get a new one.</>}
          </p>
        </div>
      ) : (
        <form className="card login-form" method="post" action="/api/auth/confirm">
          <input type="hidden" name="t" value={t} />
          <p>One click to finish creating your account.</p>
          <button type="submit" className="login-submit">Confirm my email</button>
        </form>
      )}
      <p className="muted small"><Link href="/">← Main dashboard</Link></p>
    </div>
  );
}
