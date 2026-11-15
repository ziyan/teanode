package com.teanode.app.settings

import com.teanode.app.R

// What the iPhone calls each field of the server's settings, and the sentence under it. Generated from the iPhone's forms; a field with no words here shows the server's own name.
internal object ServerFieldWords {
    fun section(section: String): Int? = when (section) {
        "identity" -> R.string.settings_server_section_identity
        "smtp" -> R.string.settings_server_section_smtp
        "resolver" -> R.string.settings_server_section_resolver
        "session" -> R.string.settings_server_section_session
        "passkey" -> R.string.settings_server_section_passkey
        "listen" -> R.string.settings_server_section_listen
        "storage" -> R.string.settings_server_section_storage
        "geoip" -> R.string.settings_server_section_geoip
        "places" -> R.string.settings_server_section_places
        "relay" -> R.string.settings_server_section_relay
        "proxy" -> R.string.settings_server_section_proxy
        "s3" -> R.string.settings_server_section_s3
        "certificates" -> R.string.settings_server_section_certificates
        "route53" -> R.string.settings_server_section_route53
        "antivirus" -> R.string.settings_server_section_antivirus
        "antispam" -> R.string.settings_server_section_antispam
        "sso" -> R.string.settings_server_section_sso
        else -> null
    }

    fun label(section: String, field: String): Int? = when (section to field) {
        ("identity" to "dataDirectory") -> R.string.settings_server_data_directory
        ("antispam" to "bayesEnabled") -> R.string.settings_server_field_antispam_bayesEnabled
        ("antispam" to "bayesMinimumMessages") -> R.string.settings_server_field_antispam_bayesMinimumMessages
        ("antispam" to "dnsEnabled") -> R.string.settings_server_field_antispam_dnsEnabled
        ("antispam" to "enabled") -> R.string.settings_server_field_antispam_enabled
        ("antispam" to "host") -> R.string.settings_server_field_antispam_host
        ("antispam" to "port") -> R.string.settings_server_field_antispam_port
        ("antispam" to "rulesEnabled") -> R.string.settings_server_field_antispam_rulesEnabled
        ("antispam" to "signalsEnabled") -> R.string.settings_server_field_antispam_signalsEnabled
        ("antivirus" to "enabled") -> R.string.settings_server_field_antivirus_enabled
        ("antivirus" to "host") -> R.string.settings_server_field_antivirus_host
        ("antivirus" to "port") -> R.string.settings_server_field_antivirus_port
        ("certificates" to "acmeDirectoryUrl") -> R.string.settings_server_field_certificates_acmeDirectoryUrl
        ("certificates" to "acmeEmail") -> R.string.settings_server_field_certificates_acmeEmail
        ("certificates" to "acmeEnabled") -> R.string.settings_server_field_certificates_acmeEnabled
        ("certificates" to "certificateFile") -> R.string.settings_server_field_certificates_certificateFile
        ("certificates" to "hosts") -> R.string.settings_server_field_certificates_hosts
        ("certificates" to "perDomain") -> R.string.settings_server_field_certificates_perDomain
        ("certificates" to "privateKeyFile") -> R.string.settings_server_field_certificates_privateKeyFile
        ("geoip" to "databaseFile") -> R.string.settings_server_field_geoip_databaseFile
        ("geoip" to "enabled") -> R.string.settings_server_field_geoip_enabled
        ("identity" to "externalAddresses") -> R.string.settings_server_field_identity_externalAddresses
        ("identity" to "mailServers") -> R.string.settings_server_field_identity_mailServers
        ("identity" to "name") -> R.string.settings_server_field_identity_name
        ("listen" to "debug") -> R.string.settings_server_field_listen_debug
        ("listen" to "http") -> R.string.settings_server_field_listen_http
        ("listen" to "https") -> R.string.settings_server_field_listen_https
        ("listen" to "imap") -> R.string.settings_server_field_listen_imap
        ("listen" to "imaps") -> R.string.settings_server_field_listen_imaps
        ("listen" to "smtpIncoming") -> R.string.settings_server_field_listen_smtpIncoming
        ("listen" to "smtpOutgoing") -> R.string.settings_server_field_listen_smtpOutgoing
        ("passkey" to "displayName") -> R.string.settings_server_field_passkey_displayName
        ("passkey" to "enabled") -> R.string.settings_server_field_passkey_enabled
        ("passkey" to "maximumPerUser") -> R.string.settings_server_field_passkey_maximumPerUser
        ("passkey" to "origins") -> R.string.settings_server_field_passkey_origins
        ("passkey" to "relyingPartyId") -> R.string.settings_server_field_passkey_relyingPartyId
        ("places" to "mapStyleUrl") -> R.string.settings_server_field_places_mapStyleUrl
        ("places" to "providerUrl") -> R.string.settings_server_field_places_providerUrl
        ("proxy" to "socks5") -> R.string.settings_server_field_proxy_socks5
        ("relay" to "enabled") -> R.string.settings_server_field_relay_enabled
        ("relay" to "host") -> R.string.settings_server_field_relay_host
        ("relay" to "password") -> R.string.settings_server_field_relay_password
        ("relay" to "port") -> R.string.settings_server_field_relay_port
        ("relay" to "username") -> R.string.settings_server_field_relay_username
        ("resolver" to "checkInterval") -> R.string.settings_server_field_resolver_checkInterval
        ("resolver" to "externalAddressServices") -> R.string.settings_server_field_resolver_externalAddressServices
        ("resolver" to "nameserver") -> R.string.settings_server_field_resolver_nameserver
        ("route53" to "accessKeyId") -> R.string.settings_server_field_route53_accessKeyId
        ("route53" to "enabled") -> R.string.settings_server_field_route53_enabled
        ("route53" to "region") -> R.string.settings_server_field_route53_region
        ("route53" to "secretAccessKey") -> R.string.settings_server_field_route53_secretAccessKey
        ("route53" to "zoneId") -> R.string.settings_server_field_route53_zoneId
        ("s3" to "accessKeyId") -> R.string.settings_server_field_s3_accessKeyId
        ("s3" to "bucket") -> R.string.settings_server_field_s3_bucket
        ("s3" to "enabled") -> R.string.settings_server_field_s3_enabled
        ("s3" to "endpoint") -> R.string.settings_server_field_s3_endpoint
        ("s3" to "pathStyle") -> R.string.settings_server_field_s3_pathStyle
        ("s3" to "region") -> R.string.settings_server_field_s3_region
        ("s3" to "secretAccessKey") -> R.string.settings_server_field_s3_secretAccessKey
        ("session" to "lifetime") -> R.string.settings_server_field_session_lifetime
        ("smtp" to "authRateBurst") -> R.string.settings_server_field_smtp_authRateBurst
        ("smtp" to "authRateLimit") -> R.string.settings_server_field_smtp_authRateLimit
        ("smtp" to "greylistDelay") -> R.string.settings_server_field_smtp_greylistDelay
        ("smtp" to "maxMessageSize") -> R.string.settings_server_field_smtp_maxMessageSize
        ("smtp" to "maxRecipientsIncoming") -> R.string.settings_server_field_smtp_maxRecipientsIncoming
        ("smtp" to "maxRecipientsOutgoing") -> R.string.settings_server_field_smtp_maxRecipientsOutgoing
        ("smtp" to "trustedSenders") -> R.string.settings_server_field_smtp_trustedSenders
        ("storage" to "directory") -> R.string.settings_server_field_storage_directory
        ("storage" to "spoolRetention") -> R.string.settings_server_field_storage_spoolRetention
        else -> null
    }

    fun hint(section: String, field: String): Int? = when (section to field) {
        ("certificates" to "acmeEnabled") -> R.string.settings_server_cert_manual_footer
        ("certificates" to "perDomain") -> R.string.settings_server_footer_certificates
        ("antispam" to "rulesEnabled") -> R.string.settings_server_antispam_sources_footer
        ("s3" to "accessKeyId"), ("route53" to "accessKeyId") -> R.string.settings_server_credentials_footer
        ("identity" to "dataDirectory") -> R.string.settings_server_data_directory_footer
        ("certificates" to "acmeDirectoryUrl") -> R.string.settings_server_field_certificates_acmeDirectoryUrl_hint
        ("certificates" to "acmeEmail") -> R.string.settings_server_field_certificates_acmeEmail_hint
        ("certificates" to "certificateFile") -> R.string.settings_server_field_certificates_certificateFile_hint
        ("certificates" to "hosts") -> R.string.settings_server_field_certificates_hosts_hint
        ("certificates" to "privateKeyFile") -> R.string.settings_server_field_certificates_privateKeyFile_hint
        ("geoip" to "databaseFile") -> R.string.settings_server_field_geoip_databaseFile_hint
        ("identity" to "externalAddresses") -> R.string.settings_server_field_identity_externalAddresses_hint
        ("identity" to "mailServers") -> R.string.settings_server_field_identity_mailServers_hint
        ("identity" to "name") -> R.string.settings_server_field_identity_name_hint
        ("listen" to "debug") -> R.string.settings_server_field_listen_debug_hint
        ("listen" to "http") -> R.string.settings_server_field_listen_http_hint
        ("listen" to "https") -> R.string.settings_server_field_listen_https_hint
        ("listen" to "imap") -> R.string.settings_server_field_listen_imap_hint
        ("listen" to "imaps") -> R.string.settings_server_field_listen_imaps_hint
        ("listen" to "smtpIncoming") -> R.string.settings_server_field_listen_smtpIncoming_hint
        ("listen" to "smtpOutgoing") -> R.string.settings_server_field_listen_smtpOutgoing_hint
        ("passkey" to "displayName") -> R.string.settings_server_field_passkey_displayName_hint
        ("passkey" to "maximumPerUser") -> R.string.settings_server_field_passkey_maximumPerUser_hint
        ("passkey" to "origins") -> R.string.settings_server_field_passkey_origins_hint
        ("passkey" to "relyingPartyId") -> R.string.settings_server_field_passkey_relyingPartyId_hint
        ("places" to "mapStyleUrl") -> R.string.settings_server_field_places_mapStyleUrl_hint
        ("places" to "providerUrl") -> R.string.settings_server_field_places_providerUrl_hint
        ("proxy" to "socks5") -> R.string.settings_server_field_proxy_socks5_hint
        ("resolver" to "checkInterval") -> R.string.settings_server_field_resolver_checkInterval_hint
        ("resolver" to "externalAddressServices") -> R.string.settings_server_field_resolver_externalAddressServices_hint
        ("resolver" to "nameserver") -> R.string.settings_server_field_resolver_nameserver_hint
        ("s3" to "endpoint") -> R.string.settings_server_field_s3_endpoint_hint
        ("session" to "lifetime") -> R.string.settings_server_field_session_lifetime_hint
        ("smtp" to "authRateBurst") -> R.string.settings_server_field_smtp_authRateBurst_hint
        ("smtp" to "authRateLimit") -> R.string.settings_server_field_smtp_authRateLimit_hint
        ("smtp" to "greylistDelay") -> R.string.settings_server_field_smtp_greylistDelay_hint
        ("smtp" to "maxMessageSize") -> R.string.settings_server_field_smtp_maxMessageSize_hint
        ("smtp" to "maxRecipientsIncoming") -> R.string.settings_server_field_smtp_maxRecipientsIncoming_hint
        ("smtp" to "maxRecipientsOutgoing") -> R.string.settings_server_field_smtp_maxRecipientsOutgoing_hint
        ("smtp" to "trustedSenders") -> R.string.settings_server_field_smtp_trustedSenders_hint
        ("storage" to "directory") -> R.string.settings_server_field_storage_directory_hint
        ("storage" to "spoolRetention") -> R.string.settings_server_field_storage_spoolRetention_hint
        else -> null
    }
}
