package com.teanode.kit.mail

import com.teanode.kit.Connection
import com.teanode.kit.HTTPRequest
import com.teanode.kit.TeaNodeError
import com.teanode.kit.TeaNodeJson
import com.teanode.kit.Upload
import kotlinx.serialization.SerialName
import kotlinx.serialization.Serializable
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import java.util.Locale

// The management side of the server's mail, which only an operator opens: all the mail the server has handled, the
// delivery queue, the DMARC reports and the domains with their DNS records, aliases, credentials and templates.

/**
 * OperatorAccess is what the signed-in person may open on the management side, as the dashboard reads it from the
 * session: whether they hold any permission that opens it at all, and which of its pages they may open.
 */
data class OperatorAccess(
    /** Whether any permission that opens the management side is held: the session's `manages`. */
    val isOperator: Boolean,
    /** Server permissions, and domain permissions held over every domain. */
    val everywherePermissions: List<String> = emptyList(),
    val domainPermissions: List<DomainPermissions> = emptyList(),
) {
    /** DomainPermissions are the permissions held over one domain. */
    @Serializable
    data class DomainPermissions(val domainId: String, val permissions: List<String> = emptyList())

    /**
     * Whether a domain permission is held over at least one domain, or everywhere through the all-domains permission that
     * widens it, as the dashboard's hasAnywhere decides.
     */
    fun hasAnywhere(permission: String): Boolean {
        val isHeldEverywhere = everywherePermissions.any { held -> held == permission || (held.endsWith("-all") && held.removeSuffix("-all") == permission) }
        return isHeldEverywhere || domainPermissions.any { held -> held.permissions.contains(permission) }
    }

    /** The rail's rows: all mail needs mail:audit, the queue queue:manage, the reports report:read and the domains domain:manage. */
    val canAuditMail: Boolean get() = hasAnywhere("mail:audit")
    val canManageQueue: Boolean get() = hasAnywhere("queue:manage")
    val canReadReports: Boolean get() = hasAnywhere("report:read")
    val canManageDomains: Boolean get() = hasAnywhere("domain:manage")

    /** Whether any of the mail delivery pages may be opened. */
    val canOpenMailDelivery: Boolean get() = canAuditMail || canManageQueue || canReadReports || canManageDomains
}

/** ManagedTone is the color the dashboard gives a status or a verdict: good, bad, a warning, or nothing to say. */
enum class ManagedTone {
    Good, Bad, Warn, Plain;

    companion object {
        /** The dashboard's toneFor. */
        fun of(statusText: String?): ManagedTone = when ((statusText ?: "").lowercase()) {
            "pass", "delivered", "accepted", "none" -> Good
            "fail", "rejected", "dropped", "hardfail", "permerror" -> Bad
            "softfail", "neutral", "temperror", "queued", "attempted", "delayed" -> Warn
            else -> Plain
        }
    }
}

/** ManagedLocation is where an address was placed: a city and a country. */
@Serializable
data class ManagedLocation(val city: String? = null, val country: String? = null) {
    /** "City, Country", or empty when the country is not known. */
    val placeText: String
        get() = if (country.isNullOrEmpty()) "" else listOf(city ?: "", country).filter { part -> part.isNotEmpty() }.joinToString(", ")
}

/** ManagedWords are the small rules the management pages share. */
object ManagedWords {
    /** How many rows the mail and report lists ask for, as the dashboard does; filtering happens over these. */
    const val LIST_LIMIT = 500

    /** How many rows a list shows at a time, as the dashboard's tables do. */
    const val PAGE_SIZE = 50

    /** A spam score to one decimal, written the way the dashboard writes it: 3.0 as 3, -1.0 as -1. */
    fun score(score: Double): String {
        val rounded = Math.round(score * 10) / 10.0
        if (rounded == Math.rint(rounded)) return rounded.toLong().toString()
        return String.format(Locale.US, "%.1f", rounded)
    }

    /** A host name without the dot that closes it in DNS. */
    fun hostName(written: String?): String = written?.removeSuffix(".") ?: ""

    /** The name a DKIM key is published at: selector._domainkey.domain. */
    fun dkimRecordName(selector: String?, domainName: String?): String =
        if (!selector.isNullOrEmpty() && !domainName.isNullOrEmpty()) "$selector._domainkey.$domainName" else domainName ?: ""

    /** Mail server names typed into one field, comma separated. */
    fun mailServerNames(typed: String): List<String> = typed.split(',').map { name -> name.trim() }.filter { name -> name.isNotEmpty() }

    /** Whether every word typed is found, in any case, in one of the texts. */
    fun matches(searchText: String, texts: List<String?>): Boolean {
        val words = searchText.lowercase().split(Regex("""\s+""")).filter { word -> word.isNotEmpty() }
        if (words.isEmpty()) return true
        val haystack = texts.filterNotNull().joinToString(" ") { text -> text.lowercase() }
        return words.all { word -> haystack.contains(word) }
    }
}

/** MailAuthenticationResults are the checks a message went through on arrival, each with what it looked at to decide. */
@Serializable
data class MailAuthenticationResults(
    val senderMx: MXLookup? = null,
    val fromMx: MXLookup? = null,
    val spf: SPFCheck? = null,
    val dmarc: DMARCCheck? = null,
    val dkims: List<DKIMCheck>? = null,
    val arc: ARCCheck? = null,
    val spamFilter: SpamFilterCheck? = null,
    val antivirus: AntivirusCheck? = null,
    val contentFilter: ContentFilterCheck? = null,
    @SerialName("errors") val errorMessages: List<String>? = null,
) {
    /** MXLookup is where a domain the message claims receives its mail. */
    @Serializable
    data class MXLookup(val domain: String? = null, val mailServers: List<String>? = null)

    /** SPFCheck is whether the domain authorizes the address that sent it. */
    @Serializable
    data class SPFCheck(val domain: String? = null, val ip: String? = null, @SerialName("result") val spfResult: String? = null)

    /** DMARCCheck is whether the domain's own policy is satisfied. */
    @Serializable
    data class DMARCCheck(
        val domain: String? = null,
        val policy: String? = null,
        val subdomainPolicy: String? = null,
        val dkimAlignment: String? = null,
        val spfAlignment: String? = null,
        @SerialName("result") val dmarcResult: String? = null,
        /** The forwarder whose ARC seal records the message passing DMARC there, set when it failed here only because of the forwarding. */
        val passedAtForwarderDomain: String? = null,
    ) {
        /** Whether the message satisfies its sender's DMARC policy, here or at a trusted forwarder. */
        val isPassed: Boolean get() = dmarcResult?.lowercase() == "pass" || !passedAtForwarderDomain.isNullOrEmpty()
    }

    /** DKIMCheck is one signature on the message and whether it holds. */
    @Serializable
    data class DKIMCheck(val domain: String? = null, val selector: String? = null, val identifier: String? = null, @SerialName("result") val dkimResult: String? = null)

    /** ARCCheck is the sealed chain of the servers it passed through. */
    @Serializable
    data class ARCCheck(@SerialName("result") val arcResult: String? = null, @SerialName("instances") val instanceCount: Int? = null)

    /** SpamRule is one rule of the built-in filter that matched, and the points it added. */
    @Serializable
    data class SpamRule(val symbol: String, val score: Double = 0.0, @SerialName("description") val ruleDescription: String? = null)

    /** SpamFilterCheck is how the spam filter scored the message. */
    @Serializable
    data class SpamFilterCheck(
        val score: Double = 0.0,
        @SerialName("result") val spamResult: String? = null,
        val symbols: List<String>? = null,
        val checks: List<SpamRule>? = null,
    )

    /** AntivirusCheck is what the virus scanner found. */
    @Serializable
    data class AntivirusCheck(val viruses: List<String>? = null)

    /** ContentFilterCheck is the attachments refused by their kind. */
    @Serializable
    data class ContentFilterCheck(val unsafeExtensions: List<String>? = null)
}

/** AuditedMail is a message as the operator's mail pages show it: a row of the list, and with the detail fields when it is opened. */
@Serializable
data class AuditedMail(
    val id: String,
    val domainId: String? = null,
    val sender: String? = null,
    val from: String? = null,
    val subject: String? = null,
    @SerialName("status") val mailStatus: String? = null,
    @SerialName("kind") val mailKind: String? = null,
    @SerialName("size") val byteCount: Int? = null,
    val receivedAt: String? = null,
    val recipients: List<String>? = null,
    val ip: String? = null,
    @SerialName("rdns") val reverseDnsName: String? = null,
    val hello: String? = null,
    val messageId: String? = null,
    val envelopeId: String? = null,
    val tlsVersion: String? = null,
    val tlsCipherSuite: String? = null,
    val location: ManagedLocation? = null,
    val authenticationResults: MailAuthenticationResults? = null,
) {
    /** Who it is from: the From header, or the envelope sender without one. */
    val shownSender: String get() = if (!from.isNullOrEmpty()) from else sender ?: ""
}

/** AuditedMailOpens is what is known about a sent message's pictures having been fetched: a floor with false positives in it, never a count of readers. */
@Serializable
data class AuditedMailOpens(
    val mailId: String? = null,
    /** Whether the message carries a picture that could be fetched at all. */
    @SerialName("trackable") val isTrackable: Boolean = false,
    @SerialName("opened") val isOpened: Boolean = false,
    val openedAt: String? = null,
    val lastOpenedAt: String? = null,
    val openCount: Int? = null,
    val ip: String? = null,
)

/** MailDelivery is one recipient of a message and what happened about it: a row of the delivery queue, or a delivery on the message's page. */
@Serializable
data class MailDelivery(
    val id: String,
    val mailId: String? = null,
    val domainId: String? = null,
    val recipient: String? = null,
    @SerialName("kind") val deliveryKind: String? = null,
    @SerialName("status") val deliveryStatus: String? = null,
    @SerialName("size") val byteCount: Int? = null,
    @SerialName("attempts") val attemptCount: Int? = null,
    @SerialName("error") val errorMessage: String? = null,
    /** email, mailServer, webhook or smtp: how it is handed on. */
    @SerialName("method") val deliveryMethod: String? = null,
    val destination: String? = null,
    val attemptedAt: String? = null,
    val deliveredAt: String? = null,
    val droppedAt: String? = null,
    val notifiedAt: String? = null,
    val retryAt: String? = null,
    @SerialName("deliveryStatuses") val statusReports: List<StatusReport>? = null,
) {
    /** RecipientReport is what the far end said about one recipient, read from the bounce it sent. */
    @Serializable
    data class RecipientReport(
        val finalRecipient: String? = null,
        @SerialName("action") val reportAction: String? = null,
        @SerialName("status") val reportStatus: String? = null,
        val remoteMta: String? = null,
        val diagnosticCode: String? = null,
        val lastAttemptDate: String? = null,
    )

    /** StatusReport is one bounce: the server that wrote it, and what it said about each recipient. */
    @Serializable
    data class StatusReport(val reportingMta: String? = null, val recipientStatuses: List<RecipientReport>? = null)

    /** Every recipient the far end reported on, across its bounces, with the server that reported it. */
    val recipientReports: List<Pair<String?, RecipientReport>>
        get() = (statusReports ?: emptyList()).flatMap { statusReport -> (statusReport.recipientStatuses ?: emptyList()).map { report -> statusReport.reportingMta to report } }

    /** Whether it has failed at least once: put off to a later attempt, or failed outright. One still on its first pass is simply in flight. */
    val isFailing: Boolean get() = deliveryStatus == "failed" || !retryAt.isNullOrEmpty()
}

/** MailSpamTraining is the label a message was taught to the filter with, and how many messages the classifier has learned since. */
@Serializable
data class MailSpamTraining(
    /** spam or ham, or empty once forgotten. */
    @SerialName("label") val trainingLabel: String = "",
    @SerialName("learnedSpam") val learnedSpamCount: Int = 0,
    @SerialName("learnedHam") val learnedHamCount: Int = 0,
)

/** AntispamClassifier is the part of the server's spam settings the message page reads: which filter runs, and how far its classifier has learned. */
@Serializable
data class AntispamClassifier(
    val effectiveEngine: String = "",
    @SerialName("bayesEnabled") val isBayesEnabled: Boolean = false,
    @SerialName("bayesMinimumMessages") val bayesMinimumMessageCount: Int = 0,
    @SerialName("bayesLearnedSpam") val bayesLearnedSpamCount: Int = 0,
    @SerialName("bayesLearnedHam") val bayesLearnedHamCount: Int = 0,
) {
    /** Whether a message can be taught to the filter: only the built-in filter with its classifier on reads what it is taught. */
    val canBeTaught: Boolean get() = effectiveEngine == "builtin" && isBayesEnabled
}

/** AuditedMailDetail is everything the operator's page for one message shows. */
data class AuditedMailDetail(
    val mail: AuditedMail,
    val content: MailContent?,
    val opens: AuditedMailOpens?,
    val deliveries: List<MailDelivery>,
    /** spam, ham, or null when it was never taught. */
    val trainingLabel: String?,
    val classifier: AntispamClassifier?,
)

/** MailOutcome is what happened to a message, in one line, as the top of the dashboard's message page says it. */
sealed class MailOutcome {
    data class RejectedForViruses(val viruses: List<String>) : MailOutcome()
    data class RejectedAsSpam(val score: Double) : MailOutcome()
    data class RejectedByDMARC(val policy: String) : MailOutcome()
    data class RejectedWithErrors(val errorMessages: List<String>) : MailOutcome()
    data object Rejected : MailOutcome()
    data object NoDeliveries : MailOutcome()
    data class PartlyDelivered(val deliveredCount: Int, val failedCount: Int, val totalCount: Int) : MailOutcome()
    data class Delivered(val deliveredCount: Int) : MailOutcome()
    data class Pending(val pendingCount: Int) : MailOutcome()

    companion object {
        fun of(mail: AuditedMail, deliveries: List<MailDelivery>): MailOutcome {
            if (mail.mailStatus == "rejected") {
                val results = mail.authenticationResults
                val viruses = results?.antivirus?.viruses
                if (!viruses.isNullOrEmpty()) return RejectedForViruses(viruses)
                val spamFilter = results?.spamFilter
                if (spamFilter != null && spamFilter.spamResult == "fail") return RejectedAsSpam(spamFilter.score)
                val dmarc = results?.dmarc
                if (dmarc != null && !dmarc.dmarcResult.isNullOrEmpty() && ManagedTone.of(dmarc.dmarcResult) == ManagedTone.Bad && !dmarc.isPassed) {
                    return RejectedByDMARC(dmarc.policy ?: "none")
                }
                val errorMessages = results?.errorMessages
                if (!errorMessages.isNullOrEmpty()) return RejectedWithErrors(errorMessages)
                return Rejected
            }
            if (deliveries.isEmpty()) return NoDeliveries
            val deliveredCount = deliveries.count { delivery -> delivery.deliveryStatus == "delivered" }
            val failedCount = deliveries.count { delivery -> delivery.deliveryStatus == "failed" || delivery.deliveryStatus == "dropped" }
            if (failedCount > 0) return PartlyDelivered(deliveredCount, failedCount, deliveries.size)
            if (deliveredCount == deliveries.size) return Delivered(deliveredCount)
            return Pending(deliveries.size - deliveredCount)
        }
    }
}

/** MailListFilter narrows the operator's list of mail the way the dashboard's column filters do: by domain, status and kind, and by words in the sender or the subject. */
data class MailListFilter(val domainId: String? = null, val mailStatus: String? = null, val mailKind: String? = null, val searchText: String = "") {
    val isFiltering: Boolean get() = domainId != null || mailStatus != null || mailKind != null || searchText.isNotEmpty()

    fun matches(mail: AuditedMail): Boolean {
        if (domainId != null && mail.domainId != domainId) return false
        if (mailStatus != null && mail.mailStatus != mailStatus) return false
        if (mailKind != null && mail.mailKind != mailKind) return false
        return ManagedWords.matches(searchText, listOf(mail.from, mail.sender, mail.subject))
    }

    fun applied(mails: List<AuditedMail>): List<AuditedMail> = mails.filter { mail -> matches(mail) }
}

/** DeliveryQueueFilter narrows the delivery queue by domain, status and kind, and by words in the recipient or the last error. */
data class DeliveryQueueFilter(val domainId: String? = null, val deliveryStatus: String? = null, val deliveryKind: String? = null, val searchText: String = "") {
    val isFiltering: Boolean get() = domainId != null || deliveryStatus != null || deliveryKind != null || searchText.isNotEmpty()

    fun applied(deliveries: List<MailDelivery>): List<MailDelivery> = deliveries.filter { delivery ->
        if (domainId != null && delivery.domainId != domainId) return@filter false
        if (deliveryStatus != null && delivery.deliveryStatus != deliveryStatus) return@filter false
        if (deliveryKind != null && delivery.deliveryKind != deliveryKind) return@filter false
        ManagedWords.matches(searchText, listOf(delivery.recipient, delivery.errorMessage))
    }
}

/** ReportAlignment is the three states a DMARC aggregate report's row can be in: aligned on both counts, on one (which is a pass), or on neither. */
enum class ReportAlignment {
    Both, One, Neither;

    val isPassed: Boolean get() = this != Neither
}

/**
 * DMARCReport is one row of a DMARC aggregate report: somebody else telling you what they did with mail that claimed to
 * come from one of your domains. Opened, it carries the report as it arrived.
 */
@Serializable
data class DMARCReport(
    val id: String,
    val domainId: String? = null,
    val beginAt: String? = null,
    val endAt: String? = null,
    @SerialName("count") val messageCount: Int? = null,
    val ip: String? = null,
    @SerialName("rdns") val reverseDnsName: String? = null,
    val fromDomain: String? = null,
    val senderDomain: String? = null,
    val disposition: String? = null,
    @SerialName("dkimAligned") val isDkimAligned: Boolean? = null,
    @SerialName("spfAligned") val isSpfAligned: Boolean? = null,
    val location: ManagedLocation? = null,
    val feedback: Feedback? = null,
) {
    /** Feedback is the report as it arrived: who sent it, the policy they saw, and what they did with each batch of mail. */
    @Serializable
    data class Feedback(
        val organizationName: String? = null,
        val email: String? = null,
        val extraContactInfo: String? = null,
        val reportId: String? = null,
        @SerialName("begin") val beginEpochSeconds: Double? = null,
        @SerialName("end") val endEpochSeconds: Double? = null,
        @SerialName("errors") val errorMessages: List<String>? = null,
        val domain: String? = null,
        val dkimAlignment: String? = null,
        val spfAlignment: String? = null,
        val policy: String? = null,
        val subdomainPolicy: String? = null,
        @SerialName("percent") val policyPercent: Int? = null,
        val failureOptions: String? = null,
        val records: List<FeedbackRecord>? = null,
    ) {
        /** The policy as published: p=, sp=, pct= when not all of it, fo=. */
        val policyText: String
            get() {
                val parts = mutableListOf<String>()
                if (!policy.isNullOrEmpty()) parts.add("p=$policy")
                if (!subdomainPolicy.isNullOrEmpty()) parts.add("sp=$subdomainPolicy")
                if (policyPercent != null && policyPercent != 100) parts.add("pct=$policyPercent")
                if (!failureOptions.isNullOrEmpty()) parts.add("fo=$failureOptions")
                return parts.joinToString(" · ")
            }
    }

    /** FeedbackRecord is a batch of messages from one address, what the receiver decided, and why. */
    @Serializable
    data class FeedbackRecord(
        val sourceIp: String? = null,
        @SerialName("count") val messageCount: Int? = null,
        val disposition: String? = null,
        @SerialName("dkim") val dkimEvaluation: String? = null,
        @SerialName("spf") val spfEvaluation: String? = null,
        val reasonType: String? = null,
        val reasonComment: String? = null,
        val headerFrom: String? = null,
        val envelopeFrom: String? = null,
        val envelopeTo: String? = null,
        @SerialName("dkims") val dkimResults: List<DKIMResult>? = null,
        @SerialName("spfs") val spfResults: List<SPFResult>? = null,
    ) {
        /** DKIMResult is one signature the receiver checked. */
        @Serializable
        data class DKIMResult(val domain: String? = null, val selector: String? = null, @SerialName("result") val dkimResult: String? = null, val humanResult: String? = null)

        /** SPFResult is one SPF check the receiver made. */
        @Serializable
        data class SPFResult(val domain: String? = null, val scope: String? = null, @SerialName("result") val spfResult: String? = null)

        /** Whether either evaluation passed. */
        val isPassed: Boolean get() = dkimEvaluation == "pass" || spfEvaluation == "pass"
    }

    val alignment: ReportAlignment
        get() {
            val dkim = isDkimAligned ?: false
            val spf = isSpfAligned ?: false
            if (dkim && spf) return ReportAlignment.Both
            return if (dkim || spf) ReportAlignment.One else ReportAlignment.Neither
        }
}

/** ReportListFilter narrows the reports by domain, alignment and what the receiver did, and by words in the claimed domain or the sender. */
data class ReportListFilter(val domainId: String? = null, val alignment: ReportAlignment? = null, val disposition: String? = null, val searchText: String = "") {
    val isFiltering: Boolean get() = domainId != null || alignment != null || disposition != null || searchText.isNotEmpty()

    fun applied(reports: List<DMARCReport>): List<DMARCReport> = reports.filter { report ->
        if (domainId != null && report.domainId != domainId) return@filter false
        if (alignment != null && report.alignment != alignment) return@filter false
        if (disposition != null && (report.disposition ?: "") != disposition) return@filter false
        ManagedWords.matches(searchText, listOf(report.fromDomain, report.ip, report.reverseDnsName))
    }
}

/** DNSRecord is one record a domain should publish, what was found, and whether they match. */
@Serializable
data class DNSRecord(
    @SerialName("type") val recordType: String? = null,
    val name: String? = null,
    @SerialName("expected") val expectedValue: String? = null,
    /** The preference to publish an MX record's host at. */
    val priority: Int? = null,
    /** Worth publishing, but nothing breaks without it. */
    @SerialName("optional") val isOptional: Boolean? = null,
    @SerialName("found") val foundValues: List<String>? = null,
    @SerialName("verified") val isVerified: Boolean? = null,
    val purpose: String? = null,
    /** What has to be true elsewhere before this record does anything. */
    @SerialName("blocked") val blockedReason: String? = null,
) {
    val publishedValue: String get() = if (priority != null && priority != 0) "$priority ${expectedValue ?: ""}" else expectedValue ?: ""

    val isMissing: Boolean get() = isVerified != true && isOptional != true
}

@Serializable
data class DomainDNSCheck(val checkedAt: String? = null, @SerialName("error") val errorMessage: String? = null, val records: List<DNSRecord>? = null)

/** DomainDNSSummary is what a domain's DNS check says in one line. */
sealed class DomainDNSSummary {
    data object NotChecked : DomainDNSSummary()
    data class AllPublished(val recordCount: Int) : DomainDNSSummary()
    data class Missing(val missingCount: Int) : DomainDNSSummary()

    companion object {
        fun of(check: DomainDNSCheck?): DomainDNSSummary {
            val records = check?.records ?: emptyList()
            if (records.isEmpty()) return NotChecked
            val missingCount = records.count { record -> record.isMissing }
            return if (missingCount == 0) AllPublished(records.size) else Missing(missingCount)
        }
    }
}

/** DomainAlias is where mail to an address of a domain goes: a mailbox, an address, a webhook, nowhere, or a mail server. */
@Serializable
data class DomainAlias(
    val id: String,
    val pattern: String? = null,
    val comment: String? = null,
    @SerialName("kind") val aliasKind: String? = null,
    val email: String? = null,
    val webhook: String? = null,
    val mailboxId: String? = null,
    @SerialName("disabled") val isDisabled: Boolean? = null,
    val mailServer: MailServer? = null,
) {
    @Serializable
    data class MailServer(val host: String? = null, val port: Int? = null, val username: String? = null)

    /** The kinds a person may choose; a mail server alias is kept as it is and not offered to a new one. */
    val selectableKinds: List<String> get() = if (aliasKind == "mailServer") creatableKinds + "mailServer" else creatableKinds

    val editableDestination: String get() = editableDestination(aliasKind ?: "")

    fun editableDestination(kind: String): String {
        if (aliasKind != kind) return ""
        return when (kind) {
            "email" -> email ?: ""
            "webhook" -> webhook ?: ""
            else -> ""
        }
    }

    val editorKind: String get() = if (aliasKind != null && selectableKinds.contains(aliasKind)) aliasKind else ""

    companion object {
        val creatableKinds = listOf("mailbox", "email", "webhook", "null")

        fun isDestinationFilled(aliasKind: String, destination: String, mailboxId: String): Boolean = when (aliasKind) {
            "" -> false
            "email", "webhook" -> destination.isNotBlank()
            "mailbox" -> mailboxId.isNotEmpty()
            else -> true
        }
    }
}

@Serializable
data class DomainCredential(val id: String, val comment: String? = null, val alias: String? = null, @SerialName("disabled") val isDisabled: Boolean? = null)

@Serializable
data class CreatedDomainCredential(val username: String, val password: String, val host: String = "", val port: String = "")

@Serializable
data class DomainLogo(val filename: String? = null, val title: String? = null, val url: String? = null, val publicUrl: String? = null, val uploadedAt: String? = null)

/** ManagedDomain is a domain the server handles mail for, with what the operator manages about it. */
@Serializable
data class ManagedDomain(
    val id: String,
    @SerialName("domain") val domainName: String,
    val subdomain: String? = null,
    val comment: String? = null,
    val spamFilterScoreThreshold: Double? = null,
    val aliases: List<DomainAlias>? = null,
    val credentials: List<DomainCredential>? = null,
    @SerialName("records") val dnsCheck: DomainDNSCheck? = null,
    val mailServers: List<String>? = null,
    val mailHosts: List<String>? = null,
    val linkHost: String? = null,
    val linkHostname: String? = null,
    val dkimSelector: String? = null,
    val hasDkimKey: Boolean? = null,
    val logo: DomainLogo? = null,
) {
    val dnsSummary: DomainDNSSummary get() = DomainDNSSummary.of(dnsCheck)
}

/** DomainOverview is what a domain's first page shows: how much mail, the newest, and the deliveries that are waiting. */
data class DomainOverview(val mailCountByStatus: Map<String, Int>, val newestReceivedAt: String?, val pendingDeliveries: List<MailDelivery>) {
    val totalMailCount: Int get() = mailCountByStatus.values.sum()
    val acceptedMailCount: Int get() = mailCountByStatus["accepted"] ?: 0
    val rejectedMailCount: Int get() = mailCountByStatus["rejected"] ?: 0
    val failingDeliveryCount: Int get() = pendingDeliveries.count { delivery -> delivery.isFailing }
}

/** ServerMailbox is a mailbox of anybody on the server, to choose as where an alias delivers. */
@Serializable
data class ServerMailbox(val id: String, val name: String, val userId: String? = null, val username: String, @SerialName("userName") val ownerName: String? = null) {
    val pickerText: String
        get() {
            var text = username
            if (!ownerName.isNullOrEmpty() && ownerName != username) text += " ($ownerName)"
            return "$text · $name"
        }

    val rowText: String get() = "$username · $name"
}

@Serializable
data class DomainTemplate(
    val id: String,
    val domainId: String? = null,
    val layoutId: String? = null,
    val name: String,
    val comment: String? = null,
    val locale: String? = null,
    val subject: String? = null,
    val htmlContent: String? = null,
    val textContent: String? = null,
    val variables: List<String>? = null,
    val modifiedAt: String? = null,
    val translations: List<Translation>? = null,
) {
    @Serializable
    data class Translation(val locale: String, val subject: String? = null, val htmlContent: String? = null, val textContent: String? = null)

    val locales: List<String> get() = (listOf(locale ?: "") + (translations ?: emptyList()).map { translation -> translation.locale }).filter { text -> text.isNotEmpty() }
}

@Serializable
data class DomainLayout(
    val id: String,
    val domainId: String? = null,
    val comment: String? = null,
    val locale: String? = null,
    val htmlContent: String? = null,
    val textContent: String? = null,
    val modifiedAt: String? = null,
    val translations: List<Translation>? = null,
) {
    @Serializable
    data class Translation(val locale: String, val htmlContent: String? = null, val textContent: String? = null)

    val locales: List<String> get() = (listOf(locale ?: "") + (translations ?: emptyList()).map { translation -> translation.locale }).filter { text -> text.isNotEmpty() }

    companion object {
        fun templateCountByLayoutId(templates: List<DomainTemplate>): Map<String, Int> {
            val counts = HashMap<String, Int>()
            for (template in templates) {
                val layoutId = template.layoutId
                if (!layoutId.isNullOrEmpty()) counts[layoutId] = (counts[layoutId] ?: 0) + 1
            }
            return counts
        }
    }
}

// MARK: Calls

@Serializable
private class OperatorSessionAnswer(@SerialName("GetSession") val session: Session) {
    @Serializable
    class Session(val manages: Boolean? = null, val permissions: Permissions? = null)

    @Serializable
    class Permissions(val everywhere: List<String>? = null, val byDomain: List<OperatorAccess.DomainPermissions>? = null)
}

@Serializable
private class DomainNamesAnswer(@SerialName("ListDomains") val domains: List<ManagedDomain?>)

@Serializable
private class AllMailAnswer(@SerialName("ListMails") val mails: List<AuditedMail?>)

@Serializable
private class MailOpensAnswer(@SerialName("ListMailOpens") val opens: List<AuditedMailOpens?>)

@Serializable
private class AuditedMailAnswer(
    @SerialName("GetSpamTraining") val training: Training? = null,
    @SerialName("GetSettings") val settings: Settings? = null,
    @SerialName("GetMail") val mail: AuditedMail,
    @SerialName("GetMailContent") val content: MailContent? = null,
    @SerialName("GetMailOpens") val opens: AuditedMailOpens? = null,
    @SerialName("ListDeliveriesByMail") val deliveries: List<MailDelivery?>? = null,
) {
    @Serializable
    class Training(val label: String? = null)

    @Serializable
    class Settings(val antispam: AntispamClassifier? = null)
}

@Serializable
private class MarkMailAnswer(@SerialName("MarkMail") val training: MailSpamTraining)

@Serializable
private class ForgetMailAnswer(@SerialName("ForgetMail") val training: MailSpamTraining)

@Serializable
private class PendingDeliveriesAnswer(@SerialName("ListPendingDeliveries") val deliveries: List<MailDelivery?>)

@Serializable
private class ReportsAnswer(@SerialName("ListReports") val reports: List<DMARCReport?>)

@Serializable
private class ReportAnswer(@SerialName("GetReport") val report: DMARCReport? = null)

@Serializable
private class DomainsAnswer(@SerialName("ListDomains") val domains: List<ManagedDomain?>)

@Serializable
private class CreatedDomainAnswer(@SerialName("CreateDomain") val created: Created) {
    @Serializable
    class Created(val id: String)
}

@Serializable
private class DomainAnswer(@SerialName("GetDomain") val domain: ManagedDomain? = null)

@Serializable
private class DomainOverviewAnswer(
    @SerialName("CountMailsBy") val facets: List<Facet?>? = null,
    @SerialName("ListMails") val newest: List<Newest?>? = null,
    @SerialName("ListPendingDeliveries") val pending: List<MailDelivery?>? = null,
) {
    @Serializable
    class Facet(@SerialName("value") val facetValue: String, @SerialName("count") val mailCount: Int)

    @Serializable
    class Newest(val receivedAt: String? = null)
}

@Serializable
private class ServerMailboxesAnswer(@SerialName("ListAllMailboxes") val mailboxes: List<ServerMailbox?>)

@Serializable
private class CreatedCredentialAnswer(@SerialName("CreateCredential") val created: CreatedDomainCredential)

@Serializable
private class TemplatesAnswer(@SerialName("ListTemplates") val templates: List<DomainTemplate?>? = null, @SerialName("ListLayouts") val layouts: List<DomainLayout?>? = null)

@Serializable
private class TemplateAnswer(@SerialName("GetTemplate") val template: DomainTemplate? = null)

@Serializable
private class LayoutAnswer(@SerialName("GetLayout") val layout: DomainLayout? = null)

/** The templates and the layouts of a domain. */
data class DomainTemplates(val templates: List<DomainTemplate>, val layouts: List<DomainLayout>)

private suspend fun Connection.mutateManaged(document: String, variables: Map<String, JsonElement>) {
    execute<JsonElement>(document, variables)
}

private fun text(value: String): JsonElement = JsonPrimitive(value)

private fun textOrNull(value: String?): JsonElement = if (value == null) JsonNull else JsonPrimitive(value)

private fun paginationOf(first: Int): JsonElement = JsonObject(mapOf("first" to JsonPrimitive(first)))

/** What the person may open of the management side. */
suspend fun Connection.operatorAccess(): OperatorAccess {
    val session = execute<OperatorSessionAnswer>(ManageMailDocuments.MANAGE_OPERATOR_ACCESS).session
    return OperatorAccess(session.manages ?: false, session.permissions?.everywhere ?: emptyList(), session.permissions?.byDomain ?: emptyList())
}

suspend fun Connection.isOperator(): Boolean = operatorAccess().isOperator

suspend fun Connection.managedDomainNames(): List<ManagedDomain> = execute<DomainNamesAnswer>(ManageMailDocuments.MANAGE_DOMAIN_NAMES).domains.filterNotNull()

suspend fun Connection.allMail(first: Int = ManagedWords.LIST_LIMIT): List<AuditedMail> =
    execute<AllMailAnswer>(ManageMailDocuments.MANAGE_ALL_MAIL, mapOf("domainId" to text(""), "pagination" to paginationOf(first))).mails.filterNotNull()

suspend fun Connection.mailOpens(mailIds: List<String>): Map<String, AuditedMailOpens> {
    if (mailIds.isEmpty()) return emptyMap()
    val opens = execute<MailOpensAnswer>(ManageMailDocuments.MANAGE_MAIL_OPENS, mapOf("mailIds" to strings(mailIds))).opens.filterNotNull()
    val byMailId = LinkedHashMap<String, AuditedMailOpens>()
    for (open in opens) byMailId.putIfAbsent(open.mailId ?: "", open)
    return byMailId
}

suspend fun Connection.auditedMail(mailId: String): AuditedMailDetail {
    val answer = execute<AuditedMailAnswer>(ManageMailDocuments.MANAGE_MAIL, mapOf("mailId" to text(mailId)))
    return AuditedMailDetail(
        answer.mail, answer.content, answer.opens, (answer.deliveries ?: emptyList()).filterNotNull(), answer.training?.label?.ifEmpty { null }, answer.settings?.antispam,
    )
}

/** Teaches the spam filter that a message is spam or ham, or makes it forget what it was taught about it. */
suspend fun Connection.teachSpamFilter(mailId: String, trainingLabel: String?): MailSpamTraining {
    if (trainingLabel != null) {
        return execute<MarkMailAnswer>(ManageMailDocuments.MANAGE_MARK_MAIL, mapOf("mailId" to text(mailId), "label" to text(trainingLabel))).training
    }
    return execute<ForgetMailAnswer>(ManageMailDocuments.MANAGE_FORGET_MAIL, mapOf("mailId" to text(mailId))).training
}

suspend fun Connection.pendingDeliveries(): List<MailDelivery> = execute<PendingDeliveriesAnswer>(ManageMailDocuments.MANAGE_PENDING_DELIVERIES).deliveries.filterNotNull()

suspend fun Connection.retryDelivery(deliveryId: String) {
    mutateManaged(ManageMailDocuments.MANAGE_RETRY_DELIVERY, mapOf("deliveryId" to text(deliveryId)))
}

suspend fun Connection.dmarcReports(first: Int = ManagedWords.LIST_LIMIT): List<DMARCReport> =
    execute<ReportsAnswer>(ManageMailDocuments.MANAGE_REPORTS, mapOf("pagination" to paginationOf(first))).reports.filterNotNull()

suspend fun Connection.dmarcReport(reportId: String): DMARCReport? = execute<ReportAnswer>(ManageMailDocuments.MANAGE_REPORT, mapOf("reportId" to text(reportId))).report

suspend fun Connection.managedDomains(): List<ManagedDomain> = execute<DomainsAnswer>(ManageMailDocuments.MANAGE_DOMAINS).domains.filterNotNull()

suspend fun Connection.createManagedDomain(domainName: String): String =
    execute<CreatedDomainAnswer>(ManageMailDocuments.MANAGE_CREATE_DOMAIN, mapOf("domain" to text(domainName))).created.id

suspend fun Connection.managedDomain(domainId: String): ManagedDomain? = execute<DomainAnswer>(ManageMailDocuments.MANAGE_DOMAIN, mapOf("domainId" to text(domainId))).domain

suspend fun Connection.domainOverview(domainId: String): DomainOverview {
    val answer = execute<DomainOverviewAnswer>(ManageMailDocuments.MANAGE_DOMAIN_OVERVIEW, mapOf("domainId" to text(domainId)))
    val counts = LinkedHashMap<String, Int>()
    for (facet in (answer.facets ?: emptyList()).filterNotNull()) counts[facet.facetValue] = (counts[facet.facetValue] ?: 0) + facet.mailCount
    return DomainOverview(counts, answer.newest?.filterNotNull()?.firstOrNull()?.receivedAt, (answer.pending ?: emptyList()).filterNotNull())
}

suspend fun Connection.serverMailboxes(): List<ServerMailbox> = execute<ServerMailboxesAnswer>(ManageMailDocuments.MANAGE_ALL_MAILBOXES).mailboxes.filterNotNull()

private fun aliasVariables(aliasKind: String, destination: String, mailboxId: String): Map<String, JsonElement> = mapOf(
    "kind" to text(aliasKind),
    "email" to if (aliasKind == "email") text(destination) else JsonNull,
    "webhook" to if (aliasKind == "webhook") text(destination) else JsonNull,
    "mailboxId" to if (aliasKind == "mailbox") text(mailboxId) else JsonNull,
)

suspend fun Connection.createDomainAlias(domainId: String, pattern: String, comment: String = "", aliasKind: String, destination: String, mailboxId: String) {
    mutateManaged(ManageMailDocuments.MANAGE_CREATE_ALIAS, mapOf("domainId" to text(domainId), "pattern" to text(pattern), "comment" to text(comment)) + aliasVariables(aliasKind, destination, mailboxId))
}

suspend fun Connection.updateDomainAlias(aliasId: String, pattern: String, comment: String, aliasKind: String, destination: String, mailboxId: String, isDisabled: Boolean) {
    mutateManaged(
        ManageMailDocuments.MANAGE_UPDATE_ALIAS,
        mapOf("aliasId" to text(aliasId), "pattern" to text(pattern), "comment" to text(comment), "disabled" to JsonPrimitive(isDisabled)) + aliasVariables(aliasKind, destination, mailboxId),
    )
}

suspend fun Connection.deleteDomainAlias(aliasId: String) {
    mutateManaged(ManageMailDocuments.MANAGE_DELETE_ALIAS, mapOf("aliasId" to text(aliasId)))
}

suspend fun Connection.createDomainCredential(domainId: String, comment: String): CreatedDomainCredential =
    execute<CreatedCredentialAnswer>(ManageMailDocuments.MANAGE_CREATE_CREDENTIAL, mapOf("domainId" to text(domainId), "comment" to text(comment))).created

suspend fun Connection.updateDomainCredential(credentialId: String, comment: String, restrictedLocalPart: String, isDisabled: Boolean) {
    mutateManaged(
        ManageMailDocuments.MANAGE_UPDATE_CREDENTIAL,
        mapOf("credentialId" to text(credentialId), "comment" to text(comment), "alias" to text(restrictedLocalPart), "disabled" to JsonPrimitive(isDisabled)),
    )
}

suspend fun Connection.deleteDomainCredential(credentialId: String) {
    mutateManaged(ManageMailDocuments.MANAGE_DELETE_CREDENTIAL, mapOf("credentialId" to text(credentialId)))
}

suspend fun Connection.checkDomain(domainId: String) {
    mutateManaged(ManageMailDocuments.MANAGE_CHECK_DOMAIN, mapOf("domainId" to text(domainId)))
}

suspend fun Connection.updateDomainMailServers(domainId: String, mailServers: List<String>) {
    mutateManaged(ManageMailDocuments.MANAGE_UPDATE_MAIL_SERVERS, mapOf("domainId" to text(domainId), "mailServers" to strings(mailServers)))
    checkDomain(domainId)
}

suspend fun Connection.updateDomainLinkHost(domainId: String, linkHost: String) {
    mutateManaged(ManageMailDocuments.MANAGE_UPDATE_LINK_HOST, mapOf("domainId" to text(domainId), "linkHost" to text(linkHost)))
}

suspend fun Connection.updateDomainDkimSelector(domainId: String, selector: String) {
    mutateManaged(ManageMailDocuments.MANAGE_UPDATE_SELECTOR, mapOf("domainId" to text(domainId), "selector" to text(selector)))
    checkDomain(domainId)
}

suspend fun Connection.regenerateDomainKey(domainId: String) {
    mutateManaged(ManageMailDocuments.MANAGE_REGENERATE_KEY, mapOf("domainId" to text(domainId)))
}

suspend fun Connection.deleteDomainLogo(domainId: String) {
    mutateManaged(ManageMailDocuments.MANAGE_DELETE_DOMAIN_LOGO, mapOf("domainId" to text(domainId)))
    checkDomain(domainId)
}

@Serializable
private class UploadRefusal(val error: String? = null)

/** Uploads a domain's logo, an SVG, and asks for the domain to be checked again. */
suspend fun Connection.uploadDomainLogo(domainId: String, fileName: String, svgData: ByteArray) {
    val boundary = "teanode-${java.util.UUID.randomUUID()}"
    val encodedId = java.net.URLEncoder.encode(domainId, "UTF-8").replace("+", "%20")
    val response = send(
        HTTPRequest(
            account.server.endpoint("/api/v1/domains/$encodedId/logo").toString(), "POST",
            mapOf("Content-Type" to "multipart/form-data; boundary=$boundary"), Upload.body(boundary, fileName, "image/svg+xml", svgData),
        ),
    )
    if (response.statusCode != 200) {
        val refusal = runCatching { TeaNodeJson.decodeFromString(UploadRefusal.serializer(), response.text) }.getOrNull()
        throw TeaNodeError.Refused(refusal?.error ?: response.text.take(300))
    }
    checkDomain(domainId)
}

suspend fun Connection.domainTemplates(domainId: String): DomainTemplates {
    val answer = execute<TemplatesAnswer>(ManageMailDocuments.MANAGE_TEMPLATES, mapOf("domainId" to text(domainId)))
    return DomainTemplates((answer.templates ?: emptyList()).filterNotNull(), (answer.layouts ?: emptyList()).filterNotNull())
}

suspend fun Connection.domainTemplate(templateId: String): DomainTemplate? = execute<TemplateAnswer>(ManageMailDocuments.MANAGE_TEMPLATE, mapOf("templateId" to text(templateId))).template

suspend fun Connection.domainLayout(layoutId: String): DomainLayout? = execute<LayoutAnswer>(ManageMailDocuments.MANAGE_LAYOUT, mapOf("layoutId" to text(layoutId))).layout

suspend fun Connection.deleteDomainTemplate(templateId: String) {
    mutateManaged(ManageMailDocuments.MANAGE_DELETE_TEMPLATE, mapOf("templateId" to text(templateId)))
}

suspend fun Connection.deleteDomainLayout(layoutId: String) {
    mutateManaged(ManageMailDocuments.MANAGE_DELETE_LAYOUT, mapOf("layoutId" to text(layoutId)))
}
