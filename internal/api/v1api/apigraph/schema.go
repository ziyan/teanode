package apigraph

// The GraphQL surface is these three interfaces, implemented by *api and
// turned into a schema by reflection in internal/util/graphapi.
//
// Queries over mail, deliveries and reports read the database. Everything
// about domains, aliases and credentials reads and writes the configuration
// file through config.Store, so a change made in the dashboard ends up in
// teanode.yaml and survives a restart.

// Query is every read operation.
type Query interface {
	DomainQuery
	AliasQuery
	UserQuery
	TokenQuery
	SessionQuery
	PasskeyQuery
	SettingsQuery
	ServerQuery
	CredentialQuery
	MailQuery
	ContentQuery
	DeliveryQuery
	ReportQuery
	LayoutQuery
	TemplateQuery
}

var _ Query = &graph{}

// Mutation is every write operation.
type Mutation interface {
	DomainMutation
	AliasMutation
	UserMutation
	TokenMutation
	SessionMutation
	PasskeyMutation
	SettingsMutation
	ServerMutation
	CredentialMutation
	MailMutation
	DeliveryMutation
	ReportMutation
	LayoutMutation
	TemplateMutation
	SendMutation
}

var _ Mutation = &graph{}

// There are no subscriptions. There was one, which emitted a message with the
// identifier "test" every second and asked nobody for permission; it was a
// placeholder that nothing ever called. The WebSocket endpoint stays, because
// the schema is where a real subscription would go.
