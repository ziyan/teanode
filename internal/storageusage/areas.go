// Package storageusage measures what the database and the blob store hold,
// groups it for the operator, and fits a line through the daily samples to
// say when a measured disk fills.
package storageusage

import (
	"slices"
	"sort"
)

// Area is what a table is for, as an operator reads a breakdown of the
// database.
type Area string

const (
	AreaMail          Area = "mail"
	AreaMemory        Area = "memory"
	AreaConversations Area = "conversations"
	AreaPhotos        Area = "photos"
	AreaRecordings    Area = "recordings"
	AreaFinance       Area = "finance"
	AreaLocation      Area = "location"
	AreaHealth        Area = "health"
	AreaJournal       Area = "journal"
	AreaSearch        Area = "search"

	// AreaOther is for tables that belong to none of the above, such as
	// accounts, configuration and calendars. A table is listed under it on
	// purpose, so that a new table has to be looked at, and a test fails for
	// one that is not listed anywhere.
	AreaOther Area = "other"
)

// Areas is every area, in the order a breakdown lists them.
var Areas = []Area{
	AreaMail, AreaMemory, AreaConversations, AreaPhotos, AreaRecordings, AreaFinance,
	AreaLocation, AreaHealth, AreaJournal, AreaSearch, AreaOther,
}

// areaTables lists the tables of each area. A table's indexes and its
// out-of-line storage are counted with it. The vector tables and the mail
// embeddings are the search area, whatever they are of, because they are the
// indexes the searches run over and are far larger than the rows they point
// at.
var areaTables = map[Area][]string{
	AreaMail: {
		"delivery", "mail", "mail_insight", "mail_submission", "mail_submission_cancellation", "mailbox",
		"mailbox_app_password", "mailbox_auto_reply", "mailbox_folder", "mailbox_folder_expunge", "mailbox_item",
		"mailbox_subscription", "spam_rule_set", "spam_token", "spam_training", "thread_summary",
		"agent_reply", "agent_watched_item",
	},
	AreaMemory: {
		"agent_chunk", "agent_document", "agent_dream", "agent_edge", "agent_fact", "agent_idea", "agent_memory",
		"agent_node", "agent_record_link", "agent_record_link_dismissal", "agent_revision", "agent_source",
		"agent_source_secret", "agent_source_type", "agent_symbol",
	},
	AreaConversations: {
		"agent_attachment", "agent_computer_attachment", "agent_conversation", "agent_feed_item", "agent_interaction",
		"agent_message", "agent_message_reaction", "agent_turn", "chat", "chat_call", "chat_call_member", "chat_member",
		"chat_message", "chat_reaction",
	},
	AreaPhotos: {
		"photo", "photo_album", "photo_album_item", "photo_album_share", "photo_blob", "photo_client_alias",
		"photo_face", "photo_face_group", "photo_file", "photo_preview", "photo_upload", "photo_upload_part",
	},
	AreaRecordings: {
		"recording", "recording_audio_chunk", "recording_blob", "recording_knowledge_document",
		"recording_live_utterance", "recording_media", "recording_media_part", "recording_reference_invalidation",
		"recording_share", "recording_speaker_correction", "recording_speaker_identity",
		"recording_temporary_speaker", "recording_transcript_run", "recording_transcript_segment",
		"recording_transcript_text_correction", "recording_transcription_attempt", "recording_voice_reference",
	},
	AreaFinance: {
		"agent_asset", "agent_asset_valuation", "agent_finance_account", "agent_finance_paystub",
		"agent_finance_paystub_line", "agent_finance_receipt", "agent_finance_receipt_line",
		"agent_finance_receipt_match", "agent_finance_security", "agent_finance_tax_form",
		"agent_finance_tax_form_entry", "agent_finance_trade", "agent_finance_transaction", "agent_savings_target",
		"agent_savings_target_asset", "agent_savings_target_finance_account", "agent_spending_category",
		"agent_spending_rule", "agent_tax_year_setting", "finance_account_share", "finance_share",
	},
	AreaLocation: {
		"location_notification_mute", "location_pause", "location_permission", "location_point", "location_share",
		"location_visit_notice", "motion_activity", "place", "place_visit", "place_visit_key",
		"travel_mode_correction", "trip", "trip_backfill", "trip_leg",
	},
	AreaHealth: {
		"health_characteristic", "health_day_summary", "health_deleted_sample", "health_sample", "health_share",
		"health_type_total", "health_workout_route",
	},
	AreaJournal: {
		"journal_day", "journal_link", "journal_period",
	},
	AreaSearch: {
		"agent_chunk_vector", "agent_fact_vector", "agent_node_vector", "agent_overview_section_vector",
		"mail_embedding", "photo_vector", "vector_model",
	},
	AreaOther: {
		"acme_challenge", "addressbook", "agent", "agent_alert", "agent_alert_candidate", "agent_alert_mute",
		"agent_background_work", "agent_billed_day", "agent_billing_check", "agent_budget", "agent_channel",
		"agent_evaluation_answer", "agent_evaluation_question", "agent_evaluation_run", "agent_feedback",
		"agent_follow_up", "agent_goal_activity", "agent_goal_artifact", "agent_job", "agent_mcp_connection",
		"agent_page_share", "agent_phone", "agent_plan_usage",
		"agent_provider_access_token", "agent_provider_sign_in", "agent_reach", "agent_skill",
		"agent_skill_secret", "agent_todo", "agent_usage", "alias", "alias_usage", "audit_event", "bimi_logo",
		"bimi_publication", "calendar", "calendar_invitation", "calendar_object", "calendar_occurrence",
		"calendar_request", "calendar_request_cancellation", "calendar_share", "circle", "circle_invitation",
		"circle_invitation_send", "circle_member", "configuration", "configuration_version", "contact",
		"credential", "credential_usage", "domain", "domain_submission", "domain_usage", "exchange_rate", "group",
		"group_domain", "group_role", "layout", "layout_translation", "link_preview", "mcp_held_result", "media",
		"media_link", "migration", "oauth_authorization", "oauth_client", "oauth_connect_code", "passkey",
		"password_agent_key", "password_item", "password_item_version", "password_setup", "password_share",
		"password_share_key", "person_relationship", "person_relationship_notice", "rate_limit_bucket", "report",
		"role", "role_permission", "session", "shopping_item", "shopping_item_price", "shopping_list",
		"shopping_list_reminder_mute", "shopping_list_share", "shopping_receipt_match", "shopping_store_mute",
		"shopping_trip", "shopping_trip_list", "skill_share", "skill_share_call", "storage_disk_warning",
		"storage_usage_sample", "sync_credential", "template", "template_translation", "throttle", "token",
		"user", "user_group", "user_identity",
	},
}

var tableArea = func() map[string]Area {
	areas := map[string]Area{}
	for area, tables := range areaTables {
		for _, table := range tables {
			areas[table] = area
		}
	}
	return areas
}()

// AreaOf is the area a table belongs to, and whether the table is listed at
// all. A table that is not listed is counted as other when measured; the
// test over the migrations keeps that from happening.
func AreaOf(table string) (Area, bool) {
	area, isListed := tableArea[table]
	if !isListed {
		return AreaOther, false
	}
	return area, true
}

// ListedTables is every table the map names, sorted.
func ListedTables() []string {
	tables := make([]string, 0, len(tableArea))
	for table := range tableArea {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	return tables
}

// TablesOf is the tables listed under an area.
func TablesOf(area Area) []string {
	return slices.Clone(areaTables[area])
}
