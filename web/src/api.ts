// The dashboard talks to the server over the same GraphQL endpoint the API
// exposes. There is no client library: one fetch wrapper is all a handful of
// screens needs, and a self-hosted server should not ship a megabyte of
// dependency to display a list of messages.

export class APIError extends Error {
  readonly unauthenticated: boolean

  constructor(message: string, unauthenticated = false) {
    super(message)
    this.name = 'APIError'
    this.unauthenticated = unauthenticated
  }
}

// send posts the query, once more if the connection failed before the server
// had a chance to answer.
//
// Every request this dashboard makes is a POST, and a browser will not retry
// one of those by itself — for good reason, since it cannot know whether the
// request was acted on. A connection that failed before any reply arrived is
// the case where it can: nothing was received, so nothing happened that
// asking again would repeat, and a GraphQL query changes nothing anyway.
//
// The failure this is for: a keep-alive connection picked up at the moment
// the other end had already closed it. It is invisible in the origin's log —
// the request never arrives — and it lands as a bare "Failed to fetch" over a
// page that was fine, most often on a burst of requests after an idle spell,
// which is exactly what clicking into a domain is.
//
// Once, and only for that. A server that answered, however it answered, is a
// server whose answer we keep; retrying a real failure twice as fast is not
// help.
// Where this browser is, sent with every call so the server can tell time in
// the person's own zone when nobody is looking — a scheduled brief, a held
// reply's notification. The language goes as Accept-Language, which the
// browser sends on its own.
function locationHeaders(): Record<string, string> {
  try {
    const zone = Intl.DateTimeFormat().resolvedOptions().timeZone
    return zone ? { 'X-Timezone': zone } : {}
  } catch {
    return {}
  }
}

// A page framed by another site — the drawer the browser extension puts
// on whatever page the person is on — has no session cookie of this
// origin to send. It signs in with the token the extension posts to it,
// sent as an Authorization header on every call and in the websocket's
// first message.
let bearerToken = ''

export function signInWithToken(token: string) {
  bearerToken = token
}

// framedDrawer says whether this document is the drawer framed by the
// browser extension into another site, decided once when it loaded: a
// route change never turns the frame into the whole dashboard.
export const framedDrawer = typeof window !== 'undefined' && window.self !== window.top && window.location.pathname === '/drawer'

export function authorization(): Record<string, string> {
  return bearerToken ? { Authorization: `Bearer ${bearerToken}` } : {}
}

// A picture in the transcript and a page the agent made are fetched by the
// browser itself, which cannot be told to send a header. On the dashboard
// the session cookie carries them. Framed into another site there is no
// cookie of this origin, so the server signs an address for that one file,
// good for a few hours: the person's own token never goes into an address,
// where it would be written into every access log on the way and into
// whatever they copied the link into.
const sharedAddresses = new Map<string, Promise<string>>()

export function sharedAttachment(attachmentId: string): Promise<string> {
  const known = sharedAddresses.get(attachmentId)
  if (known) return known
  const asking = graphql<{ ShareAgentAttachment: string }>(
    'query ($attachmentId: String!) { ShareAgentAttachment(attachmentId: $attachmentId) }',
    { attachmentId },
  )
    .then((answer) => answer.ShareAgentAttachment)
    .catch((reason) => {
      // Asked for again next time rather than remembered as broken.
      sharedAddresses.delete(attachmentId)
      throw reason
    })
  sharedAddresses.set(attachmentId, asking)
  return asking
}

async function send(
  query: string,
  variables: Record<string, unknown>,
  signal: AbortSignal | undefined,
): Promise<Response> {
  const request = () =>
    fetch('/api/v1/graphql', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', ...locationHeaders(), ...authorization() },
      body: JSON.stringify({ query, variables }),
      // Given by useQuery, so a request nobody is waiting for any more is
      // dropped rather than left to finish and report.
      signal,
    })

  try {
    return await request()
  } catch (caught) {
    // A request dropped on purpose is not a connection that failed.
    if (signal?.aborted || (caught instanceof DOMException && caught.name === 'AbortError')) {
      throw caught
    }
    return await request()
  }
}

export async function graphql<T>(
  query: string,
  variables: Record<string, unknown> = {},
  signal?: AbortSignal,
): Promise<T> {
  const response = await send(query, variables, signal)

  if (response.status === 401) {
    throw new APIError('not logged in', true)
  }
  if (!response.ok) {
    throw new APIError(`the server returned ${response.status}`)
  }

  const body = await response.json()
  if (body.errors && body.errors.length > 0) {
    throw new APIError(body.errors.map((error: { message: string }) => error.message).join('; '))
  }
  return body.data as T
}

export interface Session {
  authenticated: boolean
  authenticationRequired: boolean
  username: string

  // What to call this person, when they have said. Empty otherwise, and the
  // username stands in.
  name?: string

  // Whether this server offers passkeys. The sign-in form shows the passkey
  // button only when it does.
  passkeysEnabled: boolean

  // ID of the account; empty for the console and for nobody.
  userId?: string

  // What the caller may do, resolved from their groups.
  permissions?: Permissions | null

  // Whether the caller holds any permission that opens the management side.
  manages?: boolean

  // Identity providers to offer on the sign-in page, one button each.
  ssoProviders?: { id: string; name: string }[]
}

// Permissions is what a request may do: server and all-domains permissions
// everywhere, and domain permissions by domain.
export interface Permissions {
  everywhere: string[]
  byDomain: { domainId: string; permissions: string[] }[]
}

// Logging in goes through the same GraphQL endpoint as everything else. It
// used to have REST endpoints of its own, which meant two protocols to keep
// consistent for no reason beyond the one awkward detail: a browser's
// credential is a cookie, so the reply has to set a header. The server does
// that from the resolver.

const SESSION_FIELDS =
  '{ authenticated authenticationRequired username name passkeysEnabled userId manages ssoProviders { id name } permissions { everywhere byDomain { domainId permissions } } }'

export async function getSession(): Promise<Session> {
  const data = await graphql<{ GetSession: Session }>(`query { GetSession ${SESSION_FIELDS} }`)
  return data.GetSession
}

export async function login(username: string, password: string): Promise<Session> {
  const data = await graphql<{ Login: Session }>(
    `mutation ($username: String!, $password: String!) {
       Login(username: $username, password: $password) ${SESSION_FIELDS}
     }`,
    { username, password },
  )
  return data.Login
}

// Signing in with a passkey: two round trips, the browser doing the middle bit.
//
// The options and the response are strings on purpose. They are the
// specification's own JSON, handed to the browser and handed back untouched;
// giving them a shape here would mean tracking a specification this dashboard
// has no part in.
export interface PasskeyCeremony {
  ceremonyId: string
  options: string
}

export interface PasskeyPolicy {
  enabled: boolean
  maximumPerUser: number
}

export interface Passkey {
  id: string
  name: string
  createdAt?: string
  usedAt?: string
  ip?: string
  backupEligible?: boolean
  backupState?: boolean
  transports?: string[]
}

export async function beginPasskeyAssertion(): Promise<PasskeyCeremony> {
  const data = await graphql<{ BeginPasskeyAssertion: PasskeyCeremony }>(`
    mutation {
      BeginPasskeyAssertion {
        ceremonyId
        options
      }
    }
  `)
  return data.BeginPasskeyAssertion
}

export async function finishPasskeyAssertion(ceremonyId: string, response: string): Promise<Session> {
  const data = await graphql<{ FinishPasskeyAssertion: Session }>(
    `mutation ($ceremonyId: String!, $response: String!) {
       FinishPasskeyAssertion(ceremonyId: $ceremonyId, response: $response) ${SESSION_FIELDS}
     }`,
    { ceremonyId, response },
  )
  return data.FinishPasskeyAssertion
}

// createFirstAccount claims a server that has no account yet. The server
// refuses once one exists, so this cannot be used to add a second.
export async function createFirstAccount(username: string, password: string): Promise<Session> {
  const data = await graphql<{ CreateFirstAccount: Session }>(
    `mutation ($username: String!, $password: String!) {
       CreateFirstAccount(username: $username, password: $password) ${SESSION_FIELDS}
     }`,
    { username, password },
  )
  return data.CreateFirstAccount
}

export async function changePassword(currentPassword: string, newPassword: string): Promise<Session> {
  const data = await graphql<{ ChangePassword: Session }>(
    `mutation ($currentPassword: String!, $newPassword: String!) {
       ChangePassword(currentPassword: $currentPassword, newPassword: $newPassword) ${SESSION_FIELDS}
     }`,
    { currentPassword, newPassword },
  )
  return data.ChangePassword
}

export async function logout(): Promise<void> {
  await graphql(`mutation { Logout ${SESSION_FIELDS} }`)
}

// --- the shapes the server returns -----------------------------------------

export interface DNSRecord {
  type: string
  name: string
  expected: string
  // Set for MX records only; the preference to publish the host at.
  priority?: number
  // Worth publishing, but nothing breaks without it — an AAAA, typically.
  optional?: boolean
  found?: string[]
  verified: boolean
  purpose: string
  // What has to be true elsewhere before this record does anything, when it
  // is not: a BIMI record is ignored by every receiver while the domain's
  // DMARC policy is none.
  blocked?: string
}

export interface RecordSet {
  domain: string
  records: DNSRecord[]
  checkedAt: string
  error?: string
}

export interface Alias {
  id: string
  pattern: string
  comment?: string
  kind: string
  email?: string
  webhook?: string
  mailServer?: { host: string; port: number; username?: string }
  disabled: boolean

  // Where an alias of kind "mailbox" delivers.
  mailboxId?: string
}

export interface Credential {
  id: string
  comment?: string
  alias?: string
  disabled: boolean
}

export interface ServerAddresses {
  ipv4?: string
  ipv6?: string
  error?: string
}

export interface Domain {
  id: string
  domain: string
  subdomain: string
  comment?: string
  spamFilterScoreThreshold: number
  aliases: Alias[]
  credentials: Credential[]
  records?: RecordSet
  mailServers?: string[]
  mailHosts?: string[]
  // The name written into addresses this server puts in sent mail, and the
  // name actually used once the default is applied.
  linkHost?: string
  linkHostname?: string
  dkimSelector?: string
  hasDkimKey: boolean
  // The mark this server publishes for the domain, when one has been
  // uploaded: what the BIMI record points at.
  logo?: DomainLogo
}

export interface DomainLogo {
  filename: string
  title: string
  // Where the dashboard reads it: inside the API, behind the session.
  url: string
  // Where a receiver following the DNS record finds it.
  publicUrl: string
  uploadedAt: string
}

export interface Location {
  latitude?: number
  longitude?: number
  country?: string
  city?: string
}

export interface Mail {
  id: string
  domainId?: string
  sender?: string
  from?: string
  fromName?: string
  subject?: string
  // The mailing list this message came from, when it named one: what the
  // subscriptions page groups by, and what the button that leaves it needs.
  listKey?: string
  listName?: string
  listOneClick?: boolean
  // The sending domain whose published logo this server holds, set only when
  // the message proved it came from that domain.
  logoDomain?: string
  recipients?: string[]
  status?: string
  kind?: string
  size?: number
  receivedAt?: string
  ip?: string
  rdns?: string
  hello?: string
  messageId?: string
  envelopeId?: string
  tlsVersion?: string
  tlsCipherSuite?: string
  location?: Location
  authenticationResults?: AuthenticationResults
}

export interface AuthenticationResults {
  // Where the domains this message claims actually receive mail. A sender
  // claiming to be a large provider whose domain has no mail servers is the
  // shape most forgeries have.
  senderMx?: { domain?: string; mailServers?: string[] }
  fromMx?: { domain?: string; mailServers?: string[] }
  spf?: { domain?: string; ip?: string; result?: string }
  dmarc?: {
    domain?: string
    policy?: string
    subdomainPolicy?: string
    dkimAlignment?: string
    spfAlignment?: string
    result?: string
  }
  dkims?: { domain?: string; selector?: string; identifier?: string; result?: string }[]
  arc?: { result?: string; instances?: number }
  spamFilter?: {
    score: number
    result?: string
    symbols?: string[]
    // The per-check breakdown, which only the built-in filter can produce:
    // the daemon's protocol reports names without the points each contributed.
    checks?: { symbol: string; score: number; description?: string }[]
  }
  antivirus?: { viruses?: string[] }
  contentFilter?: { unsafeExtensions?: string[] }
  errors?: string[]
}

// What the far end said when a delivery was attempted. Parsed out of the DSN
// it bounced with, which is where the actual reason lives.
export interface RecipientStatus {
  originalRecipient?: string
  finalRecipient?: string
  action?: string
  status?: string
  remoteMta?: string
  diagnosticCode?: string
  lastAttemptDate?: string
}

export interface DeliveryStatus {
  reportingMta?: string
  recipientStatuses?: RecipientStatus[]
}

export interface Delivery {
  // How the message is handed on, and where — derived from the alias.
  method?: 'email' | 'mailServer' | 'webhook' | 'smtp'
  destination?: string
  id: string
  mailId?: string
  domainId?: string
  aliasId?: string
  recipient?: string
  kind?: string
  status?: string
  size?: number
  attempts?: number
  error?: string
  attemptedAt?: string
  retryAt?: string
  deliveredAt?: string
  droppedAt?: string
  notifiedAt?: string
  deliveryStatuses?: DeliveryStatus[]
}

// A DMARC aggregate report: somebody else telling you what they did with mail
// that claimed to come from one of your domains.
export interface Report {
  id: string
  domainId?: string
  mailId?: string
  beginAt?: string
  endAt?: string
  count?: number
  ip?: string
  rdns?: string
  fromDomain?: string
  senderDomain?: string
  disposition?: string
  dkimAligned?: boolean
  spfAligned?: boolean
  location?: Location
  // The report as it arrived, once one row is opened. Who sent it, what
  // policy they saw, and what they actually did about each batch of mail.
  feedback?: Feedback
}

export interface Feedback {
  organizationName?: string
  email?: string
  extraContactInfo?: string
  reportId?: string
  begin?: number
  end?: number
  errors?: string[]
  domain?: string
  dkimAlignment?: string
  spfAlignment?: string
  policy?: string
  subdomainPolicy?: string
  percent?: number
  failureOptions?: string
  records?: FeedbackRecord[]
}

export interface FeedbackRecord {
  sourceIp?: string
  count?: number
  disposition?: string
  dkim?: string
  spf?: string
  reasonType?: string
  reasonComment?: string
  headerFrom?: string
  envelopeFrom?: string
  envelopeTo?: string
  dkims?: { domain?: string; selector?: string; result?: string; humanResult?: string }[]
  spfs?: { domain?: string; scope?: string; result?: string }[]
}

export interface Attachment {
  index: number
  filename: string
  contentType: string
  size: number
  inline: boolean
}

export interface MailContent {
  mailId: string
  available: boolean
  text?: string
  html?: string
  hasRemoteContent: boolean
  // The reader has already said to load them — for this message, or for the
  // list it came from.
  imagesAllowed?: boolean
  attachments?: Attachment[]
  headers?: { key: string; value: string }[]
  rawHeaders?: string
  size: number
}

// What is known about a sent message having been looked at.
//
// A floor with false positives in it, not a measurement. It counts fetches of
// the pictures this server put in the message: Apple Mail fetches every one
// before the recipient sees anything, and most mail programs fetch none until
// the reader asks. Never show the number without saying so.
export interface MailOpens {
  // Set when several messages are asked about at once, so an answer can be
  // matched to the row that asked.
  mailId?: string
  // Whether the message carries a picture that could be fetched at all. False
  // for a message without one, where "not opened" means nothing.
  trackable: boolean
  opened: boolean
  openedAt?: string
  lastOpenedAt?: string
  openCount?: number
  ip?: string
  userAgent?: string
}

// A template's subject and content in one locale, and the template that
// carries them. The default content is on the template itself; a translation
// is the same three fields in another language.
export interface TemplateTranslation {
  locale: string
  subject?: string
  htmlContent?: string
  textContent?: string
}

export interface Template {
  id: string
  domainId?: string
  layoutId?: string
  name: string
  comment?: string
  locale?: string
  subject?: string
  htmlContent?: string
  textContent?: string
  translations?: TemplateTranslation[]
  // What the template reads when rendered. Derived by the server from the
  // content, in every locale, and its layout's.
  variables?: string[]
  createdAt?: string
  modifiedAt?: string
}

export interface LayoutTranslation {
  locale: string
  htmlContent?: string
  textContent?: string
}

export interface Layout {
  id: string
  domainId?: string
  comment?: string
  locale?: string
  htmlContent?: string
  textContent?: string
  translations?: LayoutTranslation[]
  createdAt?: string
  modifiedAt?: string
}

// A template rendered with values filled in, by the same code that sends it.
export interface Rendered {
  subject: string
  htmlContent: string
  textContent: string
  locale: string
  variables?: string[]
}

// --- mailboxes --------------------------------------------------------------

export interface MailboxAddress {
  aliasId: string
  domainId: string
  domain: string
  localPart: string
  address: string
}

export interface MailboxRuleCondition {
  field: string
  header?: string
  operator: string
  value?: string
}

export interface MailboxRuleAction {
  kind: string
  folderId?: string
  address?: string
}

export interface MailboxRule {
  name: string
  enabled: boolean
  conditions: MailboxRuleCondition[]
  actions: MailboxRuleAction[]
  stop: boolean
}

export interface MailboxAutoReply {
  enabled: boolean
  from?: string | null
  until?: string | null
  subject: string
  text: string
  html?: string
}

export interface Mailbox {
  id: string
  userId: string
  name: string
  signatureHtml?: string
  signatureText?: string
  rules?: MailboxRule[]
  autoReply?: MailboxAutoReply | null
  addresses?: MailboxAddress[]
  // What the owner's agent may do with this mailbox, when it has been
  // granted access; null when it never was.
  agent?: MailboxAgentGrant | null
}

// The parts of a mailbox's grant to the agent that the reader needs: whether
// there is one, and whether sorting is on, which is what puts the Priority
// view in the rail.
export interface MailboxAgentGrant {
  granted: boolean
  draftReplies?: boolean
  triage?: { enabled: boolean } | null
}

// What the agent worked out about a message for this mailbox.
export interface MailInsight {
  category: string
  priority: string
  needsReply: boolean
  summary: string
  actionItems?: string[]
  notes?: string
}

export interface MailboxFolder {
  id: string
  mailboxId: string
  parentId?: string
  name: string
  kind?: string
  pinnedAt?: string
  unread: number
  total: number
}

// MailboxView is a mailbox with its folder tree, as ListMailboxes returns it.
export interface MailboxView {
  mailbox: Mailbox
  folders: MailboxFolder[]
  unread: number
  starredUnread: number
  priorityUnread: number
  // The most a message may be, in bytes; zero when there is no limit.
  maxMessageSize?: number
}

export interface MailboxItem {
  id: string
  folderId: string
  mailId: string
  mail?: Mail | null
  uid: number
  seen: boolean
  flagged: boolean
  answered: boolean
  forwarded: boolean
  draft: boolean
  addedAt: string
  // The list this arrived from, when it arrived from one.
  subscriptionId?: string
  // What the agent worked out about it, once it has.
  insight?: MailInsight | null
}

export interface MailboxItemPage {
  items: MailboxItem[]
  total: number
}

// A conversation as a folder's list shows one: the newest of its messages in
// that folder, how many there are, and who has written.
export interface MailboxThread {
  threadId: string
  item: MailboxItem
  count: number
  unread: number
  flagged: boolean
  participants: string[]
  itemIds: string[]
  hasDraft: boolean
}

export interface MailboxThreadPage {
  threads: MailboxThread[]
  total: number
}

// One message of a conversation, and the folder it is filed in.
export interface MailboxThreadItem {
  item: MailboxItem
  folderId: string
  folderName: string
  folderKind: string
}

// A conversation as it is read: every message of it in this mailbox, newest
// first, whatever folder each is in.
export interface MailboxThreadView {
  threadId: string
  subject: string
  items: MailboxThreadItem[]
  truncated: boolean
  // The agent's summary of the conversation, where the agent summarizes
  // this mailbox.
  summary?: ThreadSummary | null
  // The reply the agent is holding for this conversation, if any.
  heldReply?: AgentReply | null
}

// A reply the agent wrote on the person's behalf, and where it stands.
export interface AgentReply {
  id: string
  createdAt: string
  mailboxId: string
  mailId: string
  threadId?: string
  draftItemId?: string
  status: 'held' | 'sent' | 'cancelled' | 'refused' | 'failed'
  reason?: string
  subject: string
  from: string
  to: string
  text: string
  sendAfter?: string | null
  sentAt?: string | null
}

// A conversation's summary: the text, how far it reads, and whether a
// fresher one is being written.
export interface ThreadSummary {
  summary: string
  throughMailId: string
  messageCount: number
  createdAt: string
  stale: boolean
  pending: boolean
}

// subscribe follows a GraphQL subscription over the websocket the API
// serves on the same path, and calls back with each result. The returned
// function stops it. The socket speaks the same small protocol the server
// does: connection_init with the CSRF token, start with the document, data
// per result, ka to keep alive, stop to end.
//
// A socket that drops — a laptop lid, a phone in a pocket, a server
// restarted — comes back on its own, a little later each time, and at
// once when the network or the tab returns. Before each start, the first
// included, `beforeStart` runs and is waited for: a chance to read what
// was missed while away, so that what the subscription then replays
// lands on a fresh picture. The subscription ends for good only when the
// server says so — an error for the document, or complete — or when the
// returned function is called.
export interface SubscribeOptions {
  beforeStart?: (reconnecting: boolean) => Promise<void> | void
}

// A socket that says nothing for this long — the server says "ka" every
// second — is dead, whatever the browser thinks, and is replaced.
const SUBSCRIBE_SILENCE = 15000

export function subscribe<T>(
  query: string,
  variables: Record<string, unknown>,
  onData: (data: T) => void,
  onEnd?: (error?: Error) => void,
  options?: SubscribeOptions,
): () => void {
  const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
  const address = `${protocol}//${window.location.host}/api/v1/graphql`
  let ended = false
  let socket: WebSocket | null = null
  let retry: number | undefined
  let silence: number | undefined
  let attempts = 0
  let connections = 0
  let started = ''
  const end = (error?: Error) => {
    if (ended) {
      return
    }
    ended = true
    window.removeEventListener('online', wake)
    document.removeEventListener('visibilitychange', wake)
    window.clearTimeout(retry)
    window.clearTimeout(silence)
    onEnd?.(error)
  }
  const csrf = () => {
    const match = document.cookie.match(/(?:^|; )csrftoken=([^;]*)/)
    return match ? decodeURIComponent(match[1]) : ''
  }
  const connect = () => {
    if (ended) {
      return
    }
    retry = undefined
    const id = String(Date.now()) + Math.random().toString(36).slice(2)
    const current = new WebSocket(address)
    socket = current
    connections += 1
    const reconnecting = connections > 1
    const heard = () => {
      window.clearTimeout(silence)
      silence = window.setTimeout(() => {
        if (socket === current) current.close()
      }, SUBSCRIBE_SILENCE)
    }
    current.onopen = () => {
      heard()
      current.send(JSON.stringify({ type: 'connection_init', payload: { 'X-CSRFToken': csrf(), ...locationHeaders(), ...authorization() } }))
    }
    current.onmessage = async (event) => {
      heard()
      let message: { id?: string; type?: string; payload?: { data?: T; errors?: { message: string }[]; message?: string } }
      try {
        message = JSON.parse(String(event.data))
      } catch {
        return
      }
      switch (message.type) {
        case 'connection_ack':
          attempts = 0
          try {
            await options?.beforeStart?.(reconnecting)
          } catch {
            // What could not be read now is read when the next event
            // asks for it; the subscription starts regardless.
          }
          if (ended || socket !== current || current.readyState !== WebSocket.OPEN) return
          started = id
          current.send(JSON.stringify({ id, type: 'start', payload: { query, variables } }))
          break
        case 'data':
          if (message.payload?.errors && message.payload.errors.length > 0) {
            end(new APIError(message.payload.errors.map((error) => error.message).join('; ')))
            current.close()
            return
          }
          if (message.payload?.data) {
            onData(message.payload.data)
          }
          break
        case 'error':
          // The document itself was refused: no socket will change that.
          end(new APIError(message.payload?.message ?? 'the subscription was refused'))
          current.close()
          break
        case 'complete':
          end()
          current.close()
          break
        default:
          break
      }
    }
    current.onerror = () => {
      // The close that follows says what to do.
    }
    current.onclose = () => {
      window.clearTimeout(silence)
      if (ended || socket !== current) return
      socket = null
      attempts += 1
      const delay = Math.min(30000, 1000 * 2 ** Math.min(attempts - 1, 5)) + Math.random() * 500
      retry = window.setTimeout(connect, delay)
    }
  }
  // Back on the network, or back to the tab: no reason to keep waiting.
  const wake = () => {
    if (ended || socket || retry === undefined) return
    if (document.visibilityState === 'hidden') return
    window.clearTimeout(retry)
    connect()
  }
  window.addEventListener('online', wake)
  document.addEventListener('visibilitychange', wake)
  connect()
  return () => {
    const current = socket
    socket = null
    if (current && current.readyState === WebSocket.OPEN) {
      current.send(JSON.stringify({ id: started, type: 'stop' }))
    }
    current?.close()
    end()
  }
}

// A page pointing the agent at a thread. The drawer listens; when it is
// there it opens with a chip for the thread and says so by marking the
// event handled, and the caller sends the person to the agent page
// otherwise.
export const AGENT_ASK_EVENT = 'teanode:agent-ask'

export interface AgentReference {
  itemId?: string
  threadId?: string
  subject?: string
  from?: string
}

export interface AgentAskDetail {
  reference: AgentReference
  handled: boolean
}

export function askAgentAbout(reference: AgentReference): boolean {
  const detail: AgentAskDetail = { reference, handled: false }
  window.dispatchEvent(new CustomEvent<AgentAskDetail>(AGENT_ASK_EVENT, { detail }))
  return detail.handled
}

// A page asking the drawer to open a conversation: a run's transcript from
// the agent page. Handled the same way as a reference.
export const AGENT_OPEN_EVENT = 'teanode:agent-open'

export interface AgentOpenDetail {
  conversationId: string
  handled: boolean
}

export function openAgentConversation(conversationId: string): boolean {
  const detail: AgentOpenDetail = { conversationId, handled: false }
  window.dispatchEvent(new CustomEvent<AgentOpenDetail>(AGENT_OPEN_EVENT, { detail }))
  return detail.handled
}

// The agent changed mail — filed, flagged, drafted, sent, a rule or a
// folder made — and the pages showing mail read again. Announced by the
// drawer after such a tool answers; listened for by the mailbox.
export const MAIL_CHANGED_EVENT = 'teanode:mail-changed'

export function announceMailChanged() {
  window.dispatchEvent(new Event(MAIL_CHANGED_EVENT))
}

// What a page has open, told to the agent with every turn as "this". A
// page that shows something the address does not name — the mailing list
// on the subscriptions page — says so here; the drawer reads the address
// for the rest.
export interface AgentViewing {
  page?: string
  itemId?: string
  threadId?: string
  subject?: string
  mailboxId?: string
  mailboxName?: string
  folderId?: string
  folderName?: string
  listKey?: string
  listName?: string
}

export const VIEWING_EVENT = 'teanode:viewing'
let viewingNow: AgentViewing | null = null

export function setAgentViewing(viewing: AgentViewing | null) {
  viewingNow = viewing
  window.dispatchEvent(new Event(VIEWING_EVENT))
}

export function agentViewing(): AgentViewing | null {
  return viewingNow
}
