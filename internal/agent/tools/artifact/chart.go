package artifact

import (
	"context"
	"fmt"
	"html"
	"math"
	"strings"

	"github.com/ziyan/teanode/internal/agent/tools"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/models"
)

// A chart the model asks for by its data, drawn here as SVG: bars, a line
// or a pie, with axes and labels. Drawn here rather than by the model,
// because a page the model writes reaches for a library from another
// server and shows blank in the sandbox, and because numbers laid out by
// code are laid out right.

type chartArguments struct {
	Title  string        `json:"title"`
	Kind   string        `json:"kind"`
	Labels []string      `json:"labels"`
	Series []chartSeries `json:"series"`
	Unit   string        `json:"unit"`
}

type chartSeries struct {
	Name   string    `json:"name"`
	Values []float64 `json:"values"`
}

// The bounds of a chart.
const (
	chartPoints      = 60
	chartSeriesCount = 6
)

// chartColours are the series colours, in order; readable on white.
var chartColours = []string{"#2f6db5", "#d97706", "#16a34a", "#dc2626", "#7c3aed", "#0891b2"}

func runChart(ctx context.Context, call *tools.Call) (*tools.Result, error) {
	arguments, err := tools.DecodeArguments[chartArguments](call)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(arguments.Title)
	if title == "" {
		return nil, fmt.Errorf("a chart needs a title")
	}
	if len(arguments.Labels) == 0 || len(arguments.Labels) > chartPoints {
		return nil, fmt.Errorf("a chart takes 1 to %d labels", chartPoints)
	}
	if len(arguments.Series) == 0 || len(arguments.Series) > chartSeriesCount {
		return nil, fmt.Errorf("a chart takes 1 to %d series", chartSeriesCount)
	}
	for index, series := range arguments.Series {
		if len(series.Values) != len(arguments.Labels) {
			return nil, fmt.Errorf("series %d has %d values for %d labels", index+1, len(series.Values), len(arguments.Labels))
		}
		for _, value := range series.Values {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("series %d has a value that is not a number", index+1)
			}
		}
	}
	var drawing string
	switch arguments.Kind {
	case "bar":
		drawing = drawBars(&arguments)
	case "line":
		drawing = drawLines(&arguments)
	case "pie":
		if len(arguments.Series) != 1 {
			return nil, fmt.Errorf("a pie is one series")
		}
		drawing = drawPie(&arguments)
	default:
		return nil, fmt.Errorf("%q is not bar, line or pie", arguments.Kind)
	}
	run, err := tools.RunFrom(ctx)
	if err != nil {
		return nil, err
	}
	store := run.Storage()
	if store == nil {
		return nil, fmt.Errorf("nowhere to keep a chart")
	}
	var created *models.AgentAttachment
	if err := run.Database().TransactionContext(ctx, func(tx db.Transaction) (err error) {
		created, err = tx.CreateAgentAttachment(&models.AgentAttachment{
			AgentID:        run.Agent().ID,
			ConversationID: run.Conversation().ID,
			MessageID:      artifactMessage,
			Name:           safeFilename(title) + ".svg",
			ContentType:    "image/svg+xml",
			Size:           int64(len(drawing)),
		})
		if err != nil {
			return err
		}
		return store.PutFile(ctx, created.ID, []byte(drawing))
	}); err != nil {
		return nil, err
	}
	result, err := tools.JSONResult(map[string]any{
		"artifact_id": created.ID, "title": title, "kind": "svg",
		"url": "/api/v1/agent/attachments/" + created.ID,
	})
	if err != nil {
		return nil, err
	}
	result.Note = "drew " + title
	return result, nil
}

// The drawing's geometry, in SVG units.
const (
	chartWidth   = 720
	chartHeight  = 420
	chartLeft    = 64
	chartRight   = 24
	chartTop     = 48
	chartBottom  = 72
	chartLegendY = 26
)

func esc(text string) string { return html.EscapeString(text) }

func hasLegend(arguments *chartArguments) bool {
	return len(arguments.Series) > 1 || (len(arguments.Series) == 1 && arguments.Series[0].Name != "")
}

func format(value float64) string {
	if value == math.Trunc(value) && math.Abs(value) < 1e15 {
		return fmt.Sprintf("%.0f", value)
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", value), "0"), ".")
}

// niceMax is the top of an axis: a round number at or above the largest
// value, so the ticks read as 0, 5, 10 and not 0, 3.7, 7.4.
func niceMax(largest float64) float64 {
	if largest <= 0 {
		return 1
	}
	magnitude := math.Pow(10, math.Floor(math.Log10(largest)))
	for _, step := range []float64{1, 2, 2.5, 5, 10} {
		if top := step * magnitude; top >= largest {
			return top
		}
	}
	return 10 * magnitude
}

func header(arguments *chartArguments) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" font-family="system-ui, sans-serif" font-size="12">`, chartWidth, chartHeight, chartWidth, chartHeight)
	fmt.Fprintf(&builder, `<rect width="%d" height="%d" fill="#ffffff"/>`, chartWidth, chartHeight)
	fmt.Fprintf(&builder, `<text x="%d" y="20" font-size="15" font-weight="600" fill="#1f1f22">%s</text>`, chartLeft, esc(arguments.Title))
	if hasLegend(arguments) {
		x := chartLeft
		for index, series := range arguments.Series {
			colour := chartColours[index%len(chartColours)]
			name := series.Name
			if name == "" {
				name = fmt.Sprintf("Series %d", index+1)
			}
			fmt.Fprintf(&builder, `<rect x="%d" y="%d" width="10" height="10" fill="%s"/><text x="%d" y="%d" fill="#4b4b52">%s</text>`, x, chartLegendY+8, colour, x+14, chartLegendY+17, esc(name))
			x += 14 + 7*len(name) + 18
		}
	}
	return builder.String()
}

// axes draws the frame, the value ticks on the left and the labels along
// the bottom, and returns the scale for the values.
func axes(builder *strings.Builder, arguments *chartArguments, top float64) (plotWidth, plotHeight float64) {
	plotWidth = float64(chartWidth - chartLeft - chartRight)
	plotHeight = float64(chartHeight - chartTop - chartBottom)
	baseline := float64(chartTop) + plotHeight
	fmt.Fprintf(builder, `<line x1="%d" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#9a9aa2"/>`, chartLeft, baseline, float64(chartLeft)+plotWidth, baseline)
	for tick := 0; tick <= 5; tick++ {
		value := top * float64(tick) / 5
		y := baseline - plotHeight*float64(tick)/5
		fmt.Fprintf(builder, `<line x1="%d" y1="%.1f" x2="%.1f" y2="%.1f" stroke="#e4e4e8"/>`, chartLeft, y, float64(chartLeft)+plotWidth, y)
		fmt.Fprintf(builder, `<text x="%d" y="%.1f" text-anchor="end" fill="#6b6b73">%s</text>`, chartLeft-8, y+4, format(value))
	}
	// The unit above the axis, where there is no legend in the way; a
	// legend names the series, which says what the numbers are.
	if arguments.Unit != "" && !hasLegend(arguments) {
		fmt.Fprintf(builder, `<text x="%d" y="%d" fill="#6b6b73">%s</text>`, chartLeft, chartTop-8, esc(arguments.Unit))
	}
	count := len(arguments.Labels)
	slot := plotWidth / float64(count)
	for index, label := range arguments.Labels {
		x := float64(chartLeft) + slot*(float64(index)+0.5)
		shown := label
		if len(shown) > 14 {
			shown = shown[:13] + "…"
		}
		if count > 12 {
			fmt.Fprintf(builder, `<text x="%.1f" y="%.1f" text-anchor="end" fill="#4b4b52" transform="rotate(-45 %.1f %.1f)">%s</text>`, x, baseline+14, x, baseline+14, esc(shown))
		} else {
			fmt.Fprintf(builder, `<text x="%.1f" y="%.1f" text-anchor="middle" fill="#4b4b52">%s</text>`, x, baseline+18, esc(shown))
		}
	}
	return plotWidth, plotHeight
}

func largestOf(arguments *chartArguments) float64 {
	largest := 0.0
	for _, series := range arguments.Series {
		for _, value := range series.Values {
			if value > largest {
				largest = value
			}
		}
	}
	return largest
}

func drawBars(arguments *chartArguments) string {
	var builder strings.Builder
	builder.WriteString(header(arguments))
	top := niceMax(largestOf(arguments))
	plotWidth, plotHeight := axes(&builder, arguments, top)
	baseline := float64(chartTop) + plotHeight
	count := len(arguments.Labels)
	slot := plotWidth / float64(count)
	groups := len(arguments.Series)
	barWidth := slot * 0.7 / float64(groups)
	for index := range arguments.Labels {
		for series := range arguments.Series {
			value := math.Max(arguments.Series[series].Values[index], 0)
			height := plotHeight * value / top
			x := float64(chartLeft) + slot*float64(index) + slot*0.15 + barWidth*float64(series)
			colour := chartColours[series%len(chartColours)]
			fmt.Fprintf(&builder, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s" rx="2"><title>%s: %s</title></rect>`, x, baseline-height, barWidth-2, height, colour, esc(arguments.Labels[index]), format(arguments.Series[series].Values[index]))
			if groups == 1 && count <= 20 {
				fmt.Fprintf(&builder, `<text x="%.1f" y="%.1f" text-anchor="middle" fill="#4b4b52" font-size="11">%s</text>`, x+(barWidth-2)/2, baseline-height-4, format(arguments.Series[series].Values[index]))
			}
		}
	}
	builder.WriteString("</svg>")
	return builder.String()
}

func drawLines(arguments *chartArguments) string {
	var builder strings.Builder
	builder.WriteString(header(arguments))
	top := niceMax(largestOf(arguments))
	plotWidth, plotHeight := axes(&builder, arguments, top)
	baseline := float64(chartTop) + plotHeight
	count := len(arguments.Labels)
	slot := plotWidth / float64(count)
	for series := range arguments.Series {
		colour := chartColours[series%len(chartColours)]
		points := make([]string, 0, count)
		for index, value := range arguments.Series[series].Values {
			x := float64(chartLeft) + slot*(float64(index)+0.5)
			y := baseline - plotHeight*math.Max(value, 0)/top
			points = append(points, fmt.Sprintf("%.1f,%.1f", x, y))
		}
		fmt.Fprintf(&builder, `<polyline points="%s" fill="none" stroke="%s" stroke-width="2"/>`, strings.Join(points, " "), colour)
		for index, value := range arguments.Series[series].Values {
			x := float64(chartLeft) + slot*(float64(index)+0.5)
			y := baseline - plotHeight*math.Max(value, 0)/top
			fmt.Fprintf(&builder, `<circle cx="%.1f" cy="%.1f" r="3" fill="%s"><title>%s: %s</title></circle>`, x, y, colour, esc(arguments.Labels[index]), format(value))
		}
	}
	builder.WriteString("</svg>")
	return builder.String()
}

func drawPie(arguments *chartArguments) string {
	var builder strings.Builder
	builder.WriteString(header(arguments))
	values := arguments.Series[0].Values
	total := 0.0
	for _, value := range values {
		total += math.Max(value, 0)
	}
	centreX, centreY, radius := 220.0, float64(chartTop)+150, 130.0
	if total <= 0 {
		fmt.Fprintf(&builder, `<text x="%.1f" y="%.1f" text-anchor="middle" fill="#6b6b73">nothing to show</text></svg>`, centreX, centreY)
		return builder.String()
	}
	angle := -math.Pi / 2
	legendY := float64(chartTop) + 20
	for index, value := range values {
		share := math.Max(value, 0) / total
		if share == 0 {
			continue
		}
		next := angle + share*2*math.Pi
		colour := chartColours[index%len(chartColours)]
		if share >= 0.9999 {
			fmt.Fprintf(&builder, `<circle cx="%.1f" cy="%.1f" r="%.1f" fill="%s"/>`, centreX, centreY, radius, colour)
		} else {
			large := 0
			if share > 0.5 {
				large = 1
			}
			x1, y1 := centreX+radius*math.Cos(angle), centreY+radius*math.Sin(angle)
			x2, y2 := centreX+radius*math.Cos(next), centreY+radius*math.Sin(next)
			fmt.Fprintf(&builder, `<path d="M%.1f,%.1f L%.1f,%.1f A%.1f,%.1f 0 %d 1 %.1f,%.1f Z" fill="%s" stroke="#ffffff"><title>%s: %s</title></path>`, centreX, centreY, x1, y1, radius, radius, large, x2, y2, colour, esc(arguments.Labels[index]), format(value))
		}
		fmt.Fprintf(&builder, `<rect x="400" y="%.1f" width="12" height="12" fill="%s"/><text x="418" y="%.1f" fill="#4b4b52">%s — %s (%.0f%%)</text>`, legendY-10, colour, legendY, esc(arguments.Labels[index]), format(value), share*100)
		legendY += 20
		angle = next
	}
	builder.WriteString("</svg>")
	return builder.String()
}
