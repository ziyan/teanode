package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
	"gopkg.in/yaml.v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/ziyan/teanode/internal/agent"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/storage"
)

// NewEvaluateCommand builds "teanode-server evaluate".
func NewEvaluateCommand() *cli.Command {
	return &cli.Command{
		Name:  "evaluate",
		Usage: "measure the agent's memory, away from any running server",
		Commands: []*cli.Command{
			{
				Name:      "scenario",
				Usage:     "feed a scenario's records through filing and dreams in a database of its own, and grade its questions at each checkpoint",
				ArgsUsage: "<scenario.json>",
				Description: "Creates a database for the run on the PostgreSQL named, migrates it, and makes a person,\n" +
					"an agent and a records source in it; nothing of a running server is read or changed. The\n" +
					"models come from --models, the agent section of a server's configuration on its own. The database is kept afterwards, for inspection; drop it when\n" +
					"done. See docs/evaluation/scenarios/.",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "models", Usage: "a YAML file holding what a server configuration's agent section holds: providers, their keys and prices, and models", Required: true},
					&cli.StringFlag{Name: "database-host", Value: "127.0.0.1"},
					&cli.IntFlag{Name: "database-port", Value: 5432},
					&cli.StringFlag{Name: "database-user", Value: "teanode"},
					&cli.StringFlag{Name: "database-password", Value: "teanode", Sources: cli.EnvVars("TEANODE_EVALUATION_DATABASE_PASSWORD")},
					&cli.StringFlag{Name: "output", Usage: "the directory for the report, the records and the stored files", Required: true},
					&cli.StringFlag{Name: "from", Value: "memory,sources,both", Usage: "what each question is answered from: memory, sources, both, memory@planned, both@planned; empty for recall alone, which costs nothing"},
					&cli.FloatFlag{Name: "budget", Value: 2, Usage: "stop between steps once the run has spent this many dollars; 0 for no limit"},
				},
				Action: runEvaluateScenario,
			},
		},
	}
}

var scenarioDatabaseUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

func runEvaluateScenario(ctx context.Context, command *cli.Command) error {
	if command.Args().Len() != 1 {
		return fmt.Errorf("which scenario? teanode-server evaluate scenario <scenario.json>")
	}
	scenario, err := agent.ReadScenario(command.Args().First())
	if err != nil {
		return err
	}
	configuration, err := readScenarioModels(command.String("models"))
	if err != nil {
		return err
	}
	output := command.String("output")
	if err := os.MkdirAll(output, 0o700); err != nil {
		return err
	}
	store, err := storage.Open(&storage.Settings{Directory: filepath.Join(output, "storage")})
	if err != nil {
		return err
	}

	name := "scenario_" + strings.Trim(scenarioDatabaseUnsafe.ReplaceAllString(strings.ToLower(scenario.Name), "_"), "_") + "_" + time.Now().Format("20060102150405")
	settings := &db.Settings{
		Host: command.String("database-host"), Port: uint16(command.Int("database-port")),
		User: command.String("database-user"), Password: command.String("database-password"),
		DBName: name, SSLMode: "disable", BackendID: "scenario",
	}
	if err := createScenarioDatabase(settings); err != nil {
		return fmt.Errorf("cannot create the run's database: %w", err)
	}
	database, err := db.Open(settings)
	if err != nil {
		return err
	}
	if err := database.Migrate(); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(command.Writer, "database %s\n", name)

	var sources []string
	for _, source := range strings.Split(command.String("from"), ",") {
		if source = strings.TrimSpace(source); source != "" {
			sources = append(sources, source)
		}
	}
	report, runErr := agent.RunScenario(ctx, &agent.ScenarioSettings{
		Database: database, Storage: store, Configuration: configuration,
		Scenario: scenario, RecordsDirectory: filepath.Join(output, "records"),
		AnswerSources: sources, BudgetDollars: command.Float("budget"), Progress: command.Writer,
	})
	if report != nil {
		content, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(output, "report.json"), append(content, '\n'), 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(output, "report.md"), []byte(scenarioReportText(report, name)), 0o600); err != nil {
			return err
		}
		_, _ = fmt.Fprintf(command.Writer, "report in %s; spent %.4f\n", output, report.TotalCost)
	}
	return runErr
}

// readScenarioModels reads the agent section of a configuration on its
// own, over the defaults: a scenario run has no mail server to describe,
// and the rest of a configuration would only be checked and ignored.
func readScenarioModels(path string) (*config.Configuration, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	configuration := config.Default()
	decoder := yaml.NewDecoder(bytes.NewReader(content))
	decoder.KnownFields(true)
	if err := decoder.Decode(&configuration.Agent); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return configuration, nil
}

func createScenarioDatabase(settings *db.Settings) error {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=postgres sslmode=disable", settings.Host, settings.Port, settings.User, settings.Password)
	connection, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return err
	}
	if raw, err := connection.DB(); err == nil {
		defer func() { _ = raw.Close() }()
	}
	return connection.Exec(fmt.Sprintf("CREATE DATABASE %q", settings.DBName)).Error
}

// scenarioReportText is the report as a person reads it: a table a
// checkpoint, a row a question.
func scenarioReportText(report *agent.ScenarioReport, databaseName string) string {
	var text strings.Builder
	fmt.Fprintf(&text, "# %s\n\n", report.Scenario)
	fmt.Fprintf(&text, "Run %s to %s in database `%s`; spent %.4f.", report.StartedAt.Format(time.RFC3339), report.FinishedAt.Format(time.RFC3339), databaseName, report.TotalCost)
	if report.IsStopped {
		fmt.Fprintf(&text, " Stopped: %s.", report.StoppedWhy)
	}
	text.WriteString("\n\nModels:")
	for _, work := range []string{"default", "fast", "scan", "synthesize", "research", "embedding"} {
		if model := report.Models[work]; model != "" {
			fmt.Fprintf(&text, " %s `%s`;", work, model)
		}
	}
	text.WriteString("\n\n| Step | Kind | Seconds | Cost | Documents | Pages | Facts | Overviews |\n| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, step := range report.Steps {
		counts := step.GraphCounts
		if counts == nil {
			counts = &agent.ScenarioGraphCounts{}
		}
		fmt.Fprintf(&text, "| %s | %s | %.0f | %.4f | %d | %d | %d | %d |\n", step.ID, step.StepKind, float64(step.DurationMS)/1000, step.Cost,
			counts.DocumentCount, counts.PageCount, counts.FactCount, counts.OverviewCount)
	}
	for _, step := range report.Steps {
		if len(step.Questions) == 0 {
			continue
		}
		fmt.Fprintf(&text, "\n## %s\n\n| Question | Recall | Answers | Expected is in | Outdated is in |\n| --- | --- | --- | --- | --- |\n", step.ID)
		for _, question := range step.Questions {
			recall := "hit"
			if !question.IsRecallHit {
				recall = "miss: " + question.RecallFailure
			}
			var answers []string
			for _, answer := range question.Answers {
				answers = append(answers, answer.AnswerFrom+" "+answer.AnswerVerdict)
			}
			fmt.Fprintf(&text, "| %s | %s | %s | %s | %s |\n", question.ID, cellText(recall), strings.Join(answers, ", "),
				layersText(question.ExpectedLayers), layersText(question.OutdatedLayers))
		}
	}
	return text.String()
}

func layersText(layers []*agent.ScenarioClaimLayers) string {
	var parts []string
	for _, layer := range layers {
		var counts []string
		for name, count := range layer.LayerCounts {
			counts = append(counts, fmt.Sprintf("%s %d", name, count))
		}
		if len(counts) == 0 {
			counts = []string{"nowhere"}
		}
		sort.Strings(counts)
		parts = append(parts, cellText(layer.Claim)+": "+strings.Join(counts, ", "))
	}
	return strings.Join(parts, "; ")
}

func cellText(text string) string {
	return strings.ReplaceAll(text, "|", "/")
}
