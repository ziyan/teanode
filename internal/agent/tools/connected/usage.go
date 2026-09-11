package connected

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/models"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: "agent_usage", Family: tools.FamilyGeneral, Risk: tools.RiskRead,
				Description: "What the agent has spent: tokens and money, by day, by kind of work, by model or by mailbox, with the budget left for today. Ask for this person's own, or for every person on the server, which needs the permission to audit agents. Use it before answering a question about cost, or about why something stopped.",
				Parameters: tools.Object(map[string]any{
					"whose": tools.EnumProperty("this person's own usage, or the whole server's, which needs agent:audit", "mine", "server"),
					"by":    tools.EnumProperty("how to group the rows; one total when absent", "day", "kind", "model", "mailbox", "agent"),
					"since": tools.StringProperty("the start, as a date or a moment; thirty days ago by default"),
					"until": tools.StringProperty("the end; now by default"),
				}),
				Guidance: "agent_usage: money is what the person can act on, so lead with it and give tokens only when asked. Grouping by model is what shows where the cost is; grouping by kind shows which work is spending it.",
				Run:      runUsage,
			},
		}
	})
}

type usageRequest struct {
	Whose string `json:"whose"`
	By    string `json:"by"`
	Since string `json:"since"`
	Until string `json:"until"`
}

const documentMine = `query ($since: DateTime, $until: DateTime, $by: String) {
	AgentUsage(since: $since, until: $until, by: $by) { key cost currency totals { promptTokens completionTokens cacheReadTokens cacheWriteTokens calls } }
	ReadAgent { budget { used limit cost costLimit currency resetsAt } } }`

const documentServer = `query ($since: DateTime, $until: DateTime, $by: String) {
	AgentServerUsage(since: $since, until: $until, by: $by) { key cost currency totals { promptTokens completionTokens cacheReadTokens cacheWriteTokens calls } } }`

func runUsage(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[usageRequest](call)
	if err != nil {
		return nil, err
	}
	run := tools.MustRun(ctx)
	variables := map[string]any{}
	if by := strings.TrimSpace(arguments.By); by != "" {
		variables["by"] = by
	}
	for name, text := range map[string]string{"since": arguments.Since, "until": arguments.Until} {
		if strings.TrimSpace(text) == "" {
			continue
		}
		moment, err := tools.ParseMoment(text, tools.Location(run.Owner()))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		variables[name] = moment.Format(time.RFC3339)
	}
	if strings.EqualFold(strings.TrimSpace(arguments.Whose), "server") {
		var answer struct {
			AgentServerUsage []*usageRow `json:"AgentServerUsage"`
		}
		if err := run.Operations().Execute(ctx, documentServer, variables, &answer); err != nil {
			return nil, err
		}
		return tools.JSONResult(map[string]any{"whose": "the whole server", "usage": describeRows(answer.AgentServerUsage)})
	}
	var answer struct {
		AgentUsage []*usageRow `json:"AgentUsage"`
		ReadAgent  *struct {
			Budget map[string]any `json:"budget"`
		} `json:"ReadAgent"`
	}
	if err := run.Operations().Execute(ctx, documentMine, variables, &answer); err != nil {
		return nil, err
	}
	result := map[string]any{"whose": "this person", "usage": describeRows(answer.AgentUsage)}
	if answer.ReadAgent != nil && answer.ReadAgent.Budget != nil {
		result["budget_today"] = describeBudget(answer.ReadAgent.Budget)
	}
	return tools.JSONResult(result)
}

type usageRow struct {
	Key      string                  `json:"key"`
	Cost     float64                 `json:"cost"`
	Currency string                  `json:"currency"`
	Totals   models.AgentUsageTotals `json:"totals"`
}

// describeBudget names what each number is in, because a budget carries
// both money and tokens and an unlabelled pair of them is read as one: a
// token count was once given back to the person with a currency sign in
// front of it.
func describeBudget(budget map[string]any) map[string]any {
	currency, _ := budget["currency"].(string)
	if currency == "" {
		currency = "USD"
	}
	described := map[string]any{"resets_at": budget["resetsAt"], "currency": currency}
	if limit, ok := number(budget["costLimit"]); ok && limit > 0 {
		spent, _ := number(budget["cost"])
		described["money_spent_today"] = fmt.Sprintf("%.2f %s", spent, currency)
		described["money_limit_today"] = fmt.Sprintf("%.2f %s", limit, currency)
	}
	if limit, ok := number(budget["limit"]); ok && limit > 0 {
		used, _ := number(budget["used"])
		described["tokens_used_today"] = int64(used)
		described["tokens_limit_today"] = int64(limit)
	} else if used, ok := number(budget["used"]); ok {
		described["tokens_used_today"] = int64(used)
		described["tokens_limit_today"] = "no limit"
	}
	return described
}

func number(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case int64:
		return float64(typed), true
	case int:
		return float64(typed), true
	}
	return 0, false
}

// describeRows says the money first and the tokens beside it, because
// tokens are what a person cannot act on.
func describeRows(rows []*usageRow) []map[string]any {
	described := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entry := map[string]any{
			"cost": fmt.Sprintf("%.2f", row.Cost), "currency": row.Currency,
			"tokens": row.Totals.Total(), "calls": row.Totals.Calls,
		}
		if row.Key != "" {
			entry["key"] = row.Key
		}
		described = append(described, entry)
	}
	return described
}
