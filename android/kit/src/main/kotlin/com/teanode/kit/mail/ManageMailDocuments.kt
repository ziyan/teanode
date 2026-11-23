package com.teanode.kit.mail


/** The GraphQL documents, the same text as the iPhone app's (ios/TeaNodeKit): the server's tests check that text against its schema. */
object ManageMailDocuments {
    private const val D = "$"

    const val MANAGE_OPERATOR_ACCESS = """query { GetSession { manages permissions { everywhere byDomain { domainId permissions } } } }"""

    const val MANAGE_DOMAIN_NAMES = """{ ListDomains { id domain } }"""

    const val MANAGE_ALL_MAIL = """query (${D}domainId: String!, ${D}pagination: PaginationInput) {
  ListMails(domainId: ${D}domainId, pagination: ${D}pagination) {
    id domainId sender from subject status kind size receivedAt
  }
}"""

    const val MANAGE_MAIL_OPENS = """query (${D}mailIds: [String!]!) {
  ListMailOpens(mailIds: ${D}mailIds) { mailId trackable opened openedAt openCount }
}"""

    const val MANAGE_MAIL = """query (${D}mailId: String!) {
  GetSpamTraining(mailId: ${D}mailId) { label }
  GetSettings { antispam { effectiveEngine bayesEnabled bayesMinimumMessages bayesLearnedSpam bayesLearnedHam } }
  GetMail(mailId: ${D}mailId) {
    id sender from subject recipients status kind size receivedAt
    ip rdns hello messageId envelopeId tlsVersion tlsCipherSuite
    location { city country }
    authenticationResults {
      senderMx { domain mailServers }
      fromMx { domain mailServers }
      spf { domain ip result }
      dmarc { domain policy subdomainPolicy dkimAlignment spfAlignment result passedAtForwarderDomain }
      dkims { domain selector identifier result }
      arc { result instances }
      spamFilter { score result symbols checks { symbol score description } }
      antivirus { viruses }
      contentFilter { unsafeExtensions }
      errors
    }
  }
  GetMailContent(mailId: ${D}mailId) {
    mailId available text html hasRemoteContent imagesAllowed size rawHeaders
    headers { key value }
    attachments { index filename contentType size inline }
  }
  GetMailOpens(mailId: ${D}mailId) {
    trackable opened openedAt lastOpenedAt openCount ip
  }
  ListDeliveriesByMail(mailId: ${D}mailId) {
    id recipient kind status size attempts error method destination
    attemptedAt deliveredAt droppedAt notifiedAt retryAt
    deliveryStatuses {
      reportingMta
      recipientStatuses { finalRecipient action status remoteMta diagnosticCode lastAttemptDate }
    }
  }
}"""

    const val MANAGE_MARK_MAIL = """mutation (${D}mailId: String!, ${D}label: String!) {
  MarkMail(mailId: ${D}mailId, label: ${D}label) { mailId label learnedSpam learnedHam }
}"""

    const val MANAGE_FORGET_MAIL = """mutation (${D}mailId: String!) {
  ForgetMail(mailId: ${D}mailId) { mailId label learnedSpam learnedHam }
}"""

    const val MANAGE_PENDING_DELIVERIES = """query {
  ListPendingDeliveries(domainId: "", pagination: { first: 500 }) {
    id mailId domainId recipient kind status attempts error retryAt
  }
}"""

    const val MANAGE_RETRY_DELIVERY = """mutation (${D}deliveryId: String!) {
  RetryDelivery(deliveryId: ${D}deliveryId) { id status attempts }
}"""

    const val MANAGE_REPORTS = """query (${D}pagination: PaginationInput) {
  ListReports(domainId: "", pagination: ${D}pagination) {
    id domainId beginAt endAt count ip rdns
    fromDomain senderDomain disposition dkimAligned spfAligned
  }
}"""

    const val MANAGE_REPORT = """query (${D}reportId: String!) {
  GetReport(reportId: ${D}reportId) {
    id domainId beginAt endAt count ip rdns
    fromDomain senderDomain disposition dkimAligned spfAligned
    location { city country }
    feedback {
      organizationName email extraContactInfo reportId begin end errors
      domain dkimAlignment spfAlignment policy subdomainPolicy percent failureOptions
      records {
        sourceIp count disposition dkim spf reasonType reasonComment
        headerFrom envelopeFrom envelopeTo
        dkims { domain selector result humanResult }
        spfs { domain scope result }
      }
    }
  }
}"""

    const val MANAGE_DOMAINS = """{
  ListDomains {
    id domain subdomain comment
    aliases { id }
    credentials { id }
    records { records { type verified optional } }
  }
}"""

    const val MANAGE_CREATE_DOMAIN = """mutation (${D}domain: String!) {
  CreateDomain(domainParameters: { domain: ${D}domain }) { id domain }
}"""

    const val MANAGE_DOMAIN = """query (${D}domainId: String!) {
  GetDomain(domainId: ${D}domainId) {
    id domain subdomain comment spamFilterScoreThreshold mailServers mailHosts linkHost linkHostname dkimSelector hasDkimKey
    logo { filename title url publicUrl uploadedAt }
    aliases { id pattern comment kind email webhook mailboxId disabled mailServer { host port username } }
    credentials { id comment alias disabled }
    records { checkedAt error records { type name expected priority optional found verified purpose blocked } }
  }
}"""

    const val MANAGE_DOMAIN_OVERVIEW = """query (${D}domainId: String!) {
  CountMailsBy(domainId: ${D}domainId, field: "status") {
    value
    count
  }
  ListMails(
    domainId: ${D}domainId
    aggregations: [{ sort: [{ field: "receivedAt", direction: "descending" }] }]
    pagination: { first: 1 }
  ) {
    id
    receivedAt
  }
  ListPendingDeliveries(domainId: ${D}domainId, pagination: { first: 500 }) {
    id
    status
    retryAt
  }
}"""

    const val MANAGE_ALL_MAILBOXES = """{ ListAllMailboxes { id name userId username userName } }"""

    const val MANAGE_CREATE_ALIAS = """mutation (${D}domainId: String!, ${D}pattern: String!, ${D}comment: String, ${D}kind: String!, ${D}email: String, ${D}webhook: String, ${D}mailboxId: String) {
  CreateAlias(domainId: ${D}domainId, aliasParameters: { pattern: ${D}pattern, comment: ${D}comment, kind: ${D}kind, email: ${D}email, webhook: ${D}webhook, mailboxId: ${D}mailboxId }) {
    id
  }
}"""

    const val MANAGE_UPDATE_ALIAS = """mutation (${D}aliasId: String!, ${D}pattern: String!, ${D}comment: String, ${D}kind: String!, ${D}email: String, ${D}webhook: String, ${D}mailboxId: String, ${D}disabled: Boolean) {
  UpdateAlias(aliasId: ${D}aliasId, aliasParameters: { pattern: ${D}pattern, comment: ${D}comment, kind: ${D}kind, email: ${D}email, webhook: ${D}webhook, mailboxId: ${D}mailboxId, disabled: ${D}disabled }) {
    id
  }
}"""

    const val MANAGE_DELETE_ALIAS = """mutation (${D}aliasId: String!) { DeleteAlias(aliasId: ${D}aliasId) }"""

    const val MANAGE_CREATE_CREDENTIAL = """mutation (${D}domainId: String!, ${D}comment: String) {
  CreateCredential(domainId: ${D}domainId, credentialParameters: { comment: ${D}comment }) {
    username password host port
  }
}"""

    const val MANAGE_UPDATE_CREDENTIAL = """mutation (${D}credentialId: String!, ${D}comment: String, ${D}alias: String, ${D}disabled: Boolean) {
  UpdateCredential(credentialId: ${D}credentialId, credentialParameters: { comment: ${D}comment, alias: ${D}alias, disabled: ${D}disabled }) {
    id
  }
}"""

    const val MANAGE_DELETE_CREDENTIAL = """mutation (${D}credentialId: String!) { DeleteCredential(credentialId: ${D}credentialId) }"""

    const val MANAGE_CHECK_DOMAIN = """mutation (${D}domainId: String!) { CheckDomain(domainId: ${D}domainId) { id } }"""

    const val MANAGE_DELETE_DOMAIN_LOGO = """mutation (${D}domainId: String!) { DeleteDomainLogo(domainId: ${D}domainId) }"""

    const val MANAGE_UPDATE_MAIL_SERVERS = """mutation (${D}domainId: String!, ${D}mailServers: [String!]) {
  UpdateDomain(domainId: ${D}domainId, domainParameters: { mailServers: ${D}mailServers }) {
    id mailServers mailHosts
  }
}"""

    const val MANAGE_UPDATE_LINK_HOST = """mutation (${D}domainId: String!, ${D}linkHost: String) {
  UpdateDomain(domainId: ${D}domainId, domainParameters: { linkHost: ${D}linkHost }) {
    id linkHost linkHostname
  }
}"""

    const val MANAGE_UPDATE_SELECTOR = """mutation (${D}domainId: String!, ${D}selector: String!) {
  UpdateDomain(domainId: ${D}domainId, domainParameters: { dkimSelector: ${D}selector }) {
    id dkimSelector
  }
}"""

    const val MANAGE_REGENERATE_KEY = """mutation (${D}domainId: String!) {
  RegenerateDomainKey(domainId: ${D}domainId) { id dkimSelector hasDkimKey }
}"""

    const val MANAGE_TEMPLATES = """query (${D}domainId: String!) {
  ListTemplates(domainId: ${D}domainId) {
    id name comment locale layoutId modifiedAt variables
    translations { locale }
  }
  ListLayouts(domainId: ${D}domainId) {
    id comment locale modifiedAt
    translations { locale }
  }
}"""

    const val MANAGE_TEMPLATE = """query (${D}templateId: String!) {
  GetTemplate(templateId: ${D}templateId) {
    id domainId layoutId name comment locale subject htmlContent textContent variables modifiedAt
    translations { locale subject htmlContent textContent }
  }
}"""

    const val MANAGE_LAYOUT = """query (${D}layoutId: String!) {
  GetLayout(layoutId: ${D}layoutId) {
    id domainId comment locale htmlContent textContent modifiedAt
    translations { locale htmlContent textContent }
  }
}"""

    const val MANAGE_DELETE_TEMPLATE = """mutation (${D}templateId: String!) { DeleteTemplate(templateId: ${D}templateId) }"""

    const val MANAGE_DELETE_LAYOUT = """mutation (${D}layoutId: String!) { DeleteLayout(layoutId: ${D}layoutId) }"""

}