import { APIError, graphql } from '../api'

const STARTED = `{ GetServerStatus { startedAt } }`
const RESTART = `mutation { RestartServer { started } }`

// RESTART_TIMEOUT_MS bounds the wait. A server that has not answered in this
// long is not coming back on its own.
const RESTART_TIMEOUT_MS = 90_000

async function readStartedAt(): Promise<string> {
  const answer = await graphql<{ GetServerStatus: { startedAt: string } }>(STARTED)
  return answer.GetServerStatus?.startedAt ?? ''
}

// restartServer asks this instance to restart and waits for the process
// that replaces it. The old process answers for a moment after it is asked,
// so coming back is judged by a start time different from the one before,
// not by any answer at all. Whether it came back is the result; a server
// that refuses to restart throws its refusal.
export async function restartServer(): Promise<boolean> {
  let before = ''
  try {
    before = await readStartedAt()
  } catch {
    // Nothing to compare against; any start time will do.
  }
  try {
    const answer = await graphql<{ RestartServer: { started: boolean } }>(RESTART)
    if (answer.RestartServer && !answer.RestartServer.started) return false
  } catch (caught) {
    // A refusal is an answer, such as a server not started in a way that
    // lets it restart, and is passed on. A reply cut off half way is not:
    // the server answers and then shuts down, which is a success that looks
    // like a failure, and the wait below decides.
    if (caught instanceof APIError) throw caught
  }
  const deadline = Date.now() + RESTART_TIMEOUT_MS
  while (Date.now() < deadline) {
    await new Promise((resolve) => setTimeout(resolve, 1500))
    try {
      const now = await readStartedAt()
      if (now && now !== before) return true
    } catch {
      // Still away.
    }
  }
  return false
}
