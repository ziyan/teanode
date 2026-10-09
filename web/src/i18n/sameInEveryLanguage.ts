import type { Key } from './i18n'

// SAME_IN_EVERY_LANGUAGE is every key whose translation may read exactly as
// the English does, in one language or all of them. Anything else that does is a string that was
// copied and never translated, and both the catalogue check and the catalogue
// test fail on it. Keep it short: a key belongs here only when translating it
// would stop somebody matching the screen against a record, a header or a
// configuration file, or when it is not a word at all.
export const SAME_IN_EVERY_LANGUAGE: ReadonlySet<Key> = new Set<Key>([
  // The product's name, and the dash that stands for nothing.
  'app.name',
  'common.none',
  // Protocol names, which is what the two web listeners are called in every
  // language and in the configuration file beside them.
  'domains.dns',
  'integrations.tabDns',
  'serverSettings.listenHttp',
  'serverSettings.listenHttps',
  'domain.kindWebhook',
  'integrations.route53',
  'server.supervision.systemd',
  // A literal URL and an example address, which are not sentences.
  'integrations.endpointPlaceholder',
  'profile.emailPlaceholder',
  // An example path on a computer, which is not a sentence.
  'knowledge.recall.directoryPlaceholder',
  // Header names and DMARC's own vocabulary. They appear in mail and in DNS
  // records with these spellings, so translating them would stop somebody
  // matching what is on screen against what is in the record.
  'mailDetail.messageId',
  'mailDetail.html',
  'mailDetail.alignmentRelaxed',
  'mailDetail.alignmentStrict',
  // The name of the format a template is written in, and the two header
  // names a message is addressed with. Written this way in every client.
  'editor.html',
  'compose.carbonCopy',
  'compose.blindCarbonCopy',
  // How far the lightbox has zoomed in: a number and a percent sign, which
  // all three of these languages write the same way. It is a key rather than
  // a literal so that a language that does not can still be given one.
  'lightbox.scale',
  // The providers' own names, which is what they are called on their
  // websites and in the operator's settings in every language.
  'finance.provider.plaid',
  'finance.provider.simplefin',
])
