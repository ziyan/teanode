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
	TokenQuery
	SessionQuery
	PasskeyQuery
	SettingsQuery
	AgentSettingsQuery
	AgentQuery
	AgentAdminQuery
	AgentAskQuery
	AgentMemoryQuery
	AgentConnectionQuery
	AgentSkillQuery
	AgentChannelQuery
	AgentTabQuery
	AgentComputerQuery
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
	DomainMutation
	AliasMutation
	UserMutation
	RoleMutation
	GroupMutation
	MailboxMutation
	MailboxComposeMutation
	MailboxAppPasswordMutation
	MailboxContactMutation
	TokenMutation
	SessionMutation
	PasskeyMutation
	SettingsMutation
	AgentMutation
	AgentAdminMutation
	AgentAskMutation
	AgentMemoryMutation
	AgentConnectionMutation
	AgentSkillMutation
	AgentChannelMutation
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
}

var _ Subscription = &graph{}
