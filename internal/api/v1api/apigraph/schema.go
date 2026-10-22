package apigraph

// The GraphQL surface is these three interfaces, implemented by *api and
// turned into a schema by reflection in internal/util/graphapi.
//
// Queries over mail, deliveries and reports read the database. Everything
// about domains, aliases and credentials reads and writes the stored
// configuration through config.Store, which is itself in the database, so a
// change made in the dashboard reaches every instance and survives a restart.

// Query is every read operation.
type Query interface {
	ServiceQuery
	ServiceSetupQuery
	SyncQuery
	DeviceSetupQuery
	SendQuery
	DomainQuery
	AliasQuery
	UserQuery
	RoleQuery
	GroupQuery
	AuditQuery
	MailboxQuery
	MailboxComposeQuery
	MailboxRulesQuery
	MailboxAppPasswordQuery
	MailProgramQuery
	MailboxDirectoryQuery
	AddressBookQuery
	CalendarQuery
	CircleQuery
	LocationQuery
	HealthQuery
	PasswordsQuery
	ReminderQuery
	NoteQuery
	CalendarInvitationQuery
	TokenQuery
	AppQuery
	OAuthQuery
	SessionQuery
	PasskeyQuery
	SettingsQuery
	AgentSettingsQuery
	AgentQuery
	AgentAdminQuery
	AgentAskQuery
	AgentMemoryQuery
	AgentGraphQuery
	AgentPageShareQuery
	AgentRecordLinkQuery
	AgentCodingQuery
	AgentEvaluationQuery
	AgentIdeaQuery
	AgentAlertQuery
	AgentInteractionQuery
	AgentConnectionQuery
	AgentReachQuery
	AgentSkillQuery
	AgentSkillShareQuery
	AgentSourceTypeQuery
	AgentSourceSecretQuery
	AgentChannelQuery
	AgentPhoneQuery
	AgentTabQuery
	AgentBillingQuery
	RecordingTextCorrectionQuery
	RecordingSpeakerQuery
	RecordingQuery
	RecordingShareQuery
	PhotoQuery
	PhotoAlbumQuery
	PhotoFaceQuery
	PhotoPlaceQuery
	RecordingPlaybackQuery
	RecordingTranscriptQuery
	SearchQuery
	PersonCardQuery
	LinkPreviewQuery
	AgentVoiceQuery
	AgentComputerQuery
	AgentBackgroundQuery
	AgentHerdrQuery
	AgentBackgroundWorkQuery
	AgentGoalQuery
	FinanceQuery
	ServerQuery
	UpgradeQuery
	CredentialQuery
	MailQuery
	ContentQuery
	DeliveryQuery
	ReportQuery
	LayoutQuery
	TemplateQuery
	SpamQuery
}

var _ Query = &graph{}

// Mutation is every write operation.
type Mutation interface {
	AssistantSetupMutation
	ServiceMutation
	SyncMutation
	DeviceSetupMutation
	DomainMutation
	AliasMutation
	UserMutation
	RoleMutation
	GroupMutation
	MailboxMutation
	MailboxComposeMutation
	MailboxAppPasswordMutation
	MailboxSetupProfileMutation
	AddressBookMutation
	CalendarMutation
	CircleMutation
	LocationMutation
	HealthMutation
	PasswordsMutation
	ChatMutation
	ReminderMutation
	NoteMutation
	CalendarInvitationMutation
	TokenMutation
	AppMutation
	OAuthMutation
	SessionMutation
	ImpersonationMutation
	PasskeyMutation
	SettingsMutation
	RecordingTextCorrectionMutation
	RecordingSpeakerMutation
	RecordingMutation
	RecordingShareMutation
	PhotoMutation
	PhotoAlbumMutation
	PhotoFaceMutation
	RecordingPlaybackMutation
	RecordingTranscriptMutation
	RecordingAttachmentMutation
	RecordingMetadataMutation
	AgentMutation
	AgentAdminMutation
	AgentAskMutation
	AgentTabMutation
	AgentMemoryMutation
	AgentGraphMutation
	AgentPageShareMutation
	AgentRecordLinkMutation
	AgentCodingMutation
	AgentSpeakFirstMutation
	AgentEvaluationMutation
	AgentIdeaMutation
	AgentAlertMutation
	AgentConnectionMutation
	AgentReachMutation
	AgentSkillMutation
	AgentSkillShareMutation
	AgentProviderSignInMutation
	AgentBillingMutation
	AgentSourceTypeMutation
	AgentSourceSecretMutation
	AgentChannelMutation
	AgentPhoneMutation
	AgentBackgroundMutation
	AgentHerdrMutation
	AgentBackgroundWorkMutation
	AgentGoalMutation
	FinanceMutation
	ServerMutation
	UpgradeMutation
	CredentialMutation
	MailMutation
	DeliveryMutation
	ReportMutation
	LayoutMutation
	TemplateMutation
	SendMutation
	SpamMutation
}

var _ Mutation = &graph{}

// Subscription is what the websocket endpoint can follow: a turn of the
// agent as it happens. For a long while there were none — the one there had
// been emitted a message every second and asked nobody for permission — and
// the endpoint waited for a real one.
type Subscription interface {
	AgentSubscription
	LocationSubscription
}

var _ Subscription = &graph{}
