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
	TokenQuery
	SessionQuery
	PasskeyQuery
	SettingsQuery
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
	TokenMutation
	SessionMutation
	PasskeyMutation
	SettingsMutation
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

// There are no subscriptions. There was one, which emitted a message with the
// identifier "test" every second and asked nobody for permission; it was a
// placeholder that nothing ever called. The WebSocket endpoint stays, because
// the schema is where a real subscription would go.
