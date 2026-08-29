import { ChangePassword } from '../components/changePassword'

// A page of its own, reached from the account menu, rather than a card buried
// in Setup. Setup is about getting mail flowing; a password is about you.
//
// It does not name itself in the breadcrumb: that comes from the settings
// surface list, which every page under /settings is in.
export function ChangePasswordPage({ username }: { username: string }) {
  return <ChangePassword username={username} />
}
