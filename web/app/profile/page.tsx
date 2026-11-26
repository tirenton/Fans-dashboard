// Your profile: picture, display name, and password. Any signed-in account.
import { ProfileEditor, type Me } from "@/components/profile";
import { api } from "@/lib/api";
import { requireRole } from "@/lib/auth";

export const dynamic = "force-dynamic";
export const metadata = { title: "Your profile · AxisMixx" };

export default async function ProfilePage() {
  const s = await requireRole("user", "/profile");
  let me: Me;
  try {
    me = await api<Me>(`/api/profile/${s.name}`);
  } catch {
    me = { name: s.name, canChangePassword: false };
  }
  return (
    <div className="profile-page">
      <h1>Your profile</h1>
      <p className="lede">Your picture and name show next to your messages in the chat.</p>
      <ProfileEditor me={me} role={s.role} />
    </div>
  );
}
