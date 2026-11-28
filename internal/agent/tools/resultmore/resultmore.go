// Package resultmore reads on in a tool's result that was too long for one
// answer.
package resultmore

import (
	"context"

	"github.com/ziyan/teanode/internal/agent/tools"
)

func init() {
	tools.Register(func() []*tools.Tool {
		return []*tools.Tool{
			{
				Name: tools.ResultMoreName, Family: tools.FamilyGeneral, Core: true, Risk: tools.RiskRead,
				Description: "Read on in a tool's result that was too long for one answer. A long result ends with a line saying how long the whole result is and the result_id and offset to call this with; each call returns the next part the same way. Results are held for 30 minutes.",
				Parameters: tools.Object(map[string]any{
					"result_id": tools.StringProperty("the id the long result named"),
					"offset":    tools.IntegerProperty("where to read from, exactly as the long result named it"),
				}, "result_id", "offset"),
				Run: run,
			},
		}
	})
}

type arguments struct {
	ResultID string `json:"result_id"`
	Offset   int    `json:"offset"`
}

func run(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	parsed, err := tools.DecodeArguments[arguments](call)
	if err != nil {
		return nil, err
	}
	current, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	page, isUntrusted, err := tools.ReadResultMore(ctx, current, parsed.ResultID, parsed.Offset)
	if err != nil {
		return nil, err
	}
	return &tools.Result{Content: page, Untrusted: isUntrusted}, nil
}
