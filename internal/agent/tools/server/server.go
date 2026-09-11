// Package server is the server itself: its status, its settings, an
// upgrade.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/agent/tools/operator"
	"github.com/ziyan/teanode/internal/models"
	"gopkg.in/yaml.v3"
)

func init() {
	tools.Register(func() []*tools.Tool {
		manage := []models.Permission{models.PermissionServerManage}
		return []*tools.Tool{
			{
				Name: "server_status", Family: tools.FamilyServer, Risk: tools.RiskRead, Permissions: manage,
				Description: "This server: version, instance, uptime, whether a restart is pending, and whether a newer release exists.",
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					result, err := operator.Execute(ctx, `query { GetServerStatus { instance version commit startedAt uptimeSeconds pendingRestart } GetUpgrade { current latest available notes url checkedAt error } GetServerAddresses { ipv4 ipv6 } }`, nil)
					if err != nil {
						return nil, err
					}
					return tools.JSONResult(map[string]any{"status": result["GetServerStatus"], "upgrade": result["GetUpgrade"], "addresses": result["GetServerAddresses"]})
				},
			},
			{
				Name: "settings_get", Family: tools.FamilyServer, Risk: tools.RiskRead, Permissions: manage,
				Description: "The server's settings, with secrets redacted, by section: smtp, submission, imap, relay, antispam, antivirus, certificates, storage, sso, agent and the rest.",
				Parameters:  tools.Object(map[string]any{"section": tools.StringProperty("one section; all when absent")}),
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Section string `json:"section"`
					}](call)
					if err != nil {
						return nil, err
					}
					// Read from the configuration itself, redacted: the same view the
					// settings page shows, and the person holds server:manage or the
					// tool is not offered.
					redacted, err := tools.MustRun(ctx).Configuration().Redact()
					if err != nil {
						return nil, err
					}
					encoded, err := yaml.Marshal(redacted)
					if err != nil {
						return nil, err
					}
					var settings map[string]any
					if err := yaml.Unmarshal(encoded, &settings); err != nil {
						return nil, err
					}
					delete(settings, "database")
					if section := strings.TrimSpace(arguments.Section); section != "" {
						value, ok := settings[section]
						if !ok {
							return nil, fmt.Errorf("there is no settings section %q", section)
						}
						return tools.JSONResult(map[string]any{section: value})
					}
					return tools.JSONResult(settings)
				},
			},
			{
				Name: "settings_update", Family: tools.FamilyServer, Risk: tools.RiskDestructive, Permissions: manage,
				Description: "Change one section of the server's settings. Give the section and the fields to set, exactly as settings_get shows them; a secret left out or shown redacted is kept.",
				Parameters:  tools.Object(map[string]any{"section": tools.StringProperty("the section: smtp, submission, imap, relay, antispam, antivirus, certificates, storage, sso, proxy, upgrade, session, passkey, listen, identity, geoip, resolver, agent, s3, route53"), "values": map[string]any{"type": "object", "description": "the fields to set"}}, "section", "values"),
				Preview: func(arguments json.RawMessage) string {
					return "Change the server settings: " + strings.TrimSpace(string(arguments))
				},
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Section string         `json:"section"`
						Values  map[string]any `json:"values"`
					}](call)
					if err != nil {
						return nil, err
					}
					section := strings.TrimSpace(arguments.Section)
					inputs := map[string]string{"s3": "S3ParametersInput", "route53": "Route53ParametersInput", "antivirus": "ServiceParametersInput", "antispam": "AntispamParametersInput", "relay": "RelayParametersInput", "submission": "SubmissionParametersInput", "imap": "IMAPParametersInput", "sso": "SSOParametersInput", "proxy": "ProxyParametersInput", "upgrade": "UpgradeParametersInput", "certificates": "CertificateParametersInput", "smtp": "SMTPParametersInput", "resolver": "ResolverParametersInput", "session": "SessionParametersInput", "passkey": "PasskeyParametersInput", "listen": "ListenParametersInput", "identity": "IdentityParametersInput", "storage": "StorageParametersInput", "geoip": "GeoIPParametersInput", "agent": "AgentParametersInput"}
					input, ok := inputs[section]
					if !ok {
						return nil, fmt.Errorf("there is no settings section %q", section)
					}
					document := fmt.Sprintf(`mutation ($values: %s!) { UpdateSettings(%s: $values) { agent { enabled } } }`, input, section)
					if _, err := operator.Execute(ctx, document, map[string]any{"values": arguments.Values}); err != nil {
						return nil, err
					}
					return tools.TextResult("changed the %s settings", section), nil
				},
			},
			{
				Name: "server_upgrade", Family: tools.FamilyServer, Risk: tools.RiskOutward, Permissions: manage,
				Description: "Apply a newer release of the server, or restart it. The server goes away for a moment; it always asks first.",
				Parameters:  tools.Object(map[string]any{"action": tools.EnumProperty("upgrade to the latest, or restart", "upgrade", "restart"), "version": tools.StringProperty("for upgrade: a version, the latest by default")}, "action"),
				Preview: func(arguments json.RawMessage) string {
					return "Upgrade or restart the server: " + strings.TrimSpace(string(arguments))
				},
				Run: func(ctx context.Context, call *tools.Call) (*tools.Result, error) {
					arguments, err := tools.DecodeArguments[struct {
						Action  string `json:"action"`
						Version string `json:"version"`
					}](call)
					if err != nil {
						return nil, err
					}
					switch arguments.Action {
					case "upgrade":
						variables := map[string]any{}
						if arguments.Version != "" {
							variables["version"] = arguments.Version
						}
						result, err := operator.Execute(ctx, `mutation ($version: String) { ApplyUpgrade(version: $version) { current latest attemptedAt error } }`, variables)
						if err != nil {
							return nil, err
						}
						return tools.JSONResult(result["ApplyUpgrade"])
					case "restart":
						if _, err := operator.Execute(ctx, `mutation { RestartServer { started instance supervision } }`, nil); err != nil {
							return nil, err
						}
						return tools.TextResult("restarting"), nil
					}
					return nil, fmt.Errorf("%q is not an action of server_upgrade", arguments.Action)
				},
			},
		}
	})
}
