import { Navigate, useLocation } from 'react-router-dom'

// RedirectKeepingQuery moves an old path to its new one with the query
// string intact, which <Navigate to="/path"> alone would drop. A page that
// opens a dialog from the query (?setup=1) needs it to survive the move.
export function RedirectKeepingQuery({ to }: { to: string }) {
  const location = useLocation()
  return <Navigate to={{ pathname: to, search: location.search }} replace />
}
