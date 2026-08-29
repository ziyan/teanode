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

export async function graphql<T>(query: string, variables: Record<string, unknown> = {}): Promise<T> {
  const response = await fetch('/api/v1/graphql', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ query, variables }),
  })

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
}

// Logging in goes through the same GraphQL endpoint as everything else. It
// used to have REST endpoints of its own, which meant two protocols to keep
// consistent for no reason beyond the one awkward detail: a browser's
// credential is a cookie, so the reply has to set a header. The server does
// that from the resolver.

const SESSION_FIELDS = '{ authenticated authenticationRequired username name }'

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
  const data = await graphql<{ BeginPasskeyAssertion: PasskeyCeremony }>(
    `mutation { BeginPasskeyAssertion { ceremonyId options } }`,
  )
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
  subject?: string
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
  spamFilter?: { score: number; result?: string; symbols?: string[] }
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
