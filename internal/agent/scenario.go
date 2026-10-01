package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/computer"
	"github.com/ziyan/teanode/internal/config"
	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/llm"
	"github.com/ziyan/teanode/internal/models"
	"github.com/ziyan/teanode/internal/storage"
)

// A scenario is a changing history told in order: records filed as a
// records source would file them, dreams run between them, and
// checkpoints that ask what should be known by then. RunScenario feeds it
// through the same code a server runs, in a database of its own, and
// reports at each checkpoint what recall carried, how the answers were
// graded, and in which layer of the graph each expected and each
// outdated statement sits. docs/evaluation/scenarios/ describes the file.

// The kinds of step.
const (
	ScenarioStepRecords      = "records"
	ScenarioStepDream        = "dream"
	ScenarioStepCheckpoint   = "checkpoint"
	ScenarioStepConversation = "conversation"
)

// ScenarioAnswerFromSurvey answers a question with a survey of the whole
// graph, the way a question about a whole area is answered when asked
// for: a model call a page, then one to combine them.
const ScenarioAnswerFromSurvey = "survey"

// Scenario is the file.
type Scenario struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Steps       []*ScenarioStep `json:"steps"`
}

// ScenarioStep is one step: records to file, dreams to run, or questions
// to ask.
type ScenarioStep struct {
	ID       string `json:"id"`
	StepKind string `json:"stepKind"`

	// Records are lines in the shape a records source reads: id, kind,
	// at, author, title, text, and for chat channel and thread.
	Records []json.RawMessage `json:"records,omitempty"`

	// DreamCount is how many dreams to run one after another; more than
	// one with nothing new filed shows what repeating maintenance does.
	DreamCount int `json:"dreamCount,omitempty"`

	Questions []*ScenarioQuestion `json:"questions,omitempty"`

	// Messages are a conversation of the agent's own, filed as one and
	// remembered the way a finished conversation is: what it learned,
	// and the lessons its commands bore out.
	Messages []*ScenarioMessage `json:"messages,omitempty"`
}

// ScenarioMessage is one message of a conversation. A tool message's
// content is the tool's result as the tool returned it; it is fenced as
// a turn fences it before it is stored.
type ScenarioMessage struct {
	Role       string              `json:"role"`
	Content    string              `json:"content,omitempty"`
	ToolCalls  []*ScenarioToolCall `json:"toolCalls,omitempty"`
	ToolCallID string              `json:"toolCallId,omitempty"`
	ToolName   string              `json:"toolName,omitempty"`
}

// ScenarioToolCall is a call an assistant message makes.
type ScenarioToolCall struct {
	ID        string `json:"id"`
	ToolName  string `json:"toolName"`
	Arguments string `json:"arguments"`
}

// ScenarioQuestion is a question of the evaluation question set's shape.
// A claim with no path is met by any page.
type ScenarioQuestion struct {
	ID             string           `json:"id"`
	Question       string           `json:"question"`
	QuestionKind   string           `json:"kind"`
	Expects        []*ScenarioClaim `json:"expects"`
	Forbids        []*ScenarioClaim `json:"forbids"`
	ExpectedAnswer string           `json:"expectedAnswer"`
	OutdatedAnswer string           `json:"outdatedAnswer,omitempty"`

	// OutdatedClaims are statements that were true once and are not
	// now. Recall may carry them, as history; they are looked for in
	// every layer, to see where what is no longer true still stands as
	// current.
	OutdatedClaims []*ScenarioClaim `json:"outdatedClaims,omitempty"`
}

// ScenarioClaim is a statement recall has to carry, or must not: every
// word in one fact, on the page named or under it.
type ScenarioClaim struct {
	Path  string   `json:"path,omitempty"`
	Words []string `json:"words"`
}

// ReadScenario reads and checks a scenario file.
func ReadScenario(path string) (*Scenario, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var scenario Scenario
	if err := json.Unmarshal(content, &scenario); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	seen := map[string]bool{}
	for index, step := range scenario.Steps {
		if step.ID == "" || seen[step.ID] {
			return nil, fmt.Errorf("step %d needs an id of its own", index+1)
		}
		seen[step.ID] = true
		switch step.StepKind {
		case ScenarioStepRecords:
			if len(step.Records) == 0 {
				return nil, fmt.Errorf("step %s files no records", step.ID)
			}
		case ScenarioStepDream:
			if step.DreamCount <= 0 {
				step.DreamCount = 1
			}
		case ScenarioStepCheckpoint:
			if len(step.Questions) == 0 {
				return nil, fmt.Errorf("step %s asks nothing", step.ID)
			}
			for _, question := range step.Questions {
				if question.ID == "" || strings.TrimSpace(question.Question) == "" || strings.TrimSpace(question.ExpectedAnswer) == "" {
					return nil, fmt.Errorf("step %s: every question needs an id, the question and the expected answer", step.ID)
				}
			}
		case ScenarioStepConversation:
			if len(step.Messages) == 0 {
				return nil, fmt.Errorf("step %s has no messages", step.ID)
			}
		default:
			return nil, fmt.Errorf("step %s is of kind %q; records, conversation, dream or checkpoint", step.ID, step.StepKind)
		}
	}
	return &scenario, nil
}

// ScenarioSettings is what a run needs. The database is the run's own:
// RunScenario makes the person, the agent and the records source in it.
type ScenarioSettings struct {
	Database      db.Database
	Storage       storage.Storage
	Configuration *config.Configuration
	Scenario      *Scenario

	// RecordsDirectory is where the records are written for the reader,
	// one file a step, as a records source's folder holds them.
	RecordsDirectory string

	// AnswerSources are what each question is answered from: memory,
	// sources, both, memory@planned, both@planned. None asks recall
	// alone, which costs nothing.
	AnswerSources []string

	// BudgetDollars stops the run between steps once what it recorded as
	// spent passes it; zero is no limit.
	BudgetDollars float64

	Progress io.Writer

	// KeepRefreshTokens is handed a signed-in provider's refresh token
	// each time the service rotates it, so a run's own sign-in is written
	// back and still works for the next run.
	KeepRefreshTokens func(provider, refreshToken string)
}

// ScenarioReport is what a run found.
type ScenarioReport struct {
	Scenario    string                `json:"scenario"`
	StartedAt   time.Time             `json:"startedAt"`
	FinishedAt  time.Time             `json:"finishedAt"`
	Models      map[string]string     `json:"models"`
	Steps       []*ScenarioStepReport `json:"steps"`
	TotalCost   float64               `json:"totalCost"`
	IsStopped   bool                  `json:"isStopped"`
	StoppedWhy  string                `json:"stoppedWhy,omitempty"`
	GraphCounts *ScenarioGraphCounts  `json:"graphCounts,omitempty"`
}

// ScenarioStepReport is one step: what it cost and, for a checkpoint,
// how each question did.
type ScenarioStepReport struct {
	ID          string                    `json:"id"`
	StepKind    string                    `json:"stepKind"`
	DurationMS  int64                     `json:"durationMS"`
	Cost        float64                   `json:"cost"`
	FiledCount  int                       `json:"filedCount,omitempty"`
	GraphCounts *ScenarioGraphCounts      `json:"graphCounts,omitempty"`
	Questions   []*ScenarioQuestionReport `json:"questions,omitempty"`
}

// ScenarioGraphCounts is how big the graph is.
type ScenarioGraphCounts struct {
	DocumentCount int `json:"documentCount"`
	PageCount     int `json:"pageCount"`
	FactCount     int `json:"factCount"`
	OverviewCount int `json:"overviewCount"`
}

// ScenarioQuestionReport is one question at one checkpoint.
type ScenarioQuestionReport struct {
	ID string `json:"id"`

	// IsRecallHit is whether recall carried every expected claim and no
	// forbidden one; RecallFailure names the first that was not met.
	IsRecallHit   bool   `json:"isRecallHit"`
	RecallFailure string `json:"recallFailure,omitempty"`

	Answers []*ScenarioAnswerReport `json:"answers,omitempty"`

	// ShownLessons are the lessons a turn asking the question is shown.
	ShownLessons []string `json:"shownLessons,omitempty"`

	// ExpectedLayers and OutdatedLayers say, for each claim, in which
	// layer of the graph it is said and how many times: whether an
	// expected statement was ever learned, and where an outdated one
	// still stands.
	ExpectedLayers []*ScenarioClaimLayers `json:"expectedLayers"`
	OutdatedLayers []*ScenarioClaimLayers `json:"outdatedLayers"`
}

// ScenarioAnswerReport is one answer and its grade.
type ScenarioAnswerReport struct {
	AnswerFrom       string   `json:"answerFrom"`
	AnswerVerdict    string   `json:"answerVerdict"`
	VerdictReason    string   `json:"verdictReason"`
	AnswerText       string   `json:"answerText"`
	PlannedSearches  []string `json:"plannedSearches,omitempty"`
	Cost             float64  `json:"cost"`
	AnswerDurationMS int64    `json:"answerDurationMS"`
}

// ScenarioClaimLayers is where one claim is said.
type ScenarioClaimLayers struct {
	Claim       string         `json:"claim"`
	LayerCounts map[string]int `json:"layerCounts"`
}

// The layers a claim is looked for in.
const (
	scenarioLayerFact       = "fact"
	scenarioLayerOutdated   = "fact superseded or dormant"
	scenarioLayerSummary    = "page summary"
	scenarioLayerOverview   = "page overview"
	scenarioLayerTheme      = "theme"
	scenarioLayerReflection = "reflection"
	scenarioLayerLesson     = "lesson"
	scenarioLayerDocument   = "source document"
)

// scenarioOperations is the API as the person, in a run that has no API.
// A query finds nothing, as for a person with no mailboxes or calendars;
// a change is refused, so anything a dream would do through it shows in
// the dream's log rather than seeming to have been done.
type scenarioOperations struct{}

func (scenarioOperations) Permissions() *models.EffectivePermissions {
	return models.NewEffectivePermissions(nil)
}

func (scenarioOperations) Execute(_ context.Context, document string, _ map[string]any, _ any) error {
	if strings.HasPrefix(strings.TrimSpace(document), "query") {
		return nil
	}
	return errors.New("not available in a scenario run")
}

// RunScenario runs a scenario from the start and reports on it.
func RunScenario(ctx context.Context, settings *ScenarioSettings) (*ScenarioReport, error) {
	configuration := *settings.Configuration
	configuration.Agent.Enabled = true
	// The run's own budget is in dollars and checked between steps; the
	// daily token cap would stop a dream partway, which measures the cap.
	configuration.Agent.Limits.DailyTokensPerAgent = 0
	registry, err := llm.Open(&configuration.Agent)
	if err != nil {
		return nil, err
	}
	if settings.KeepRefreshTokens != nil {
		registry.KeepRefreshTokens(settings.KeepRefreshTokens)
	}
	worker := New(&Settings{
		Database: settings.Database, Storage: settings.Storage, Registry: registry,
		Configuration: func() *config.Configuration { return &configuration },
		Instance:      "scenario", Tick: time.Hour,
	})
	worker.SetOperationsFactory(func(context.Context, *models.User) (Operations, error) { return scenarioOperations{}, nil })

	var owner *models.User
	var found *models.Agent
	var source *models.AgentKnowledgeSource
	if err := settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if owner, err = tx.CreateUser(&models.User{Username: "scenario", Name: "Scenario Person"}); err != nil {
			return err
		}
		if found, err = tx.CreateAgent(&models.Agent{UserID: owner.ID, Enabled: true}); err != nil {
			return err
		}
		if err = tx.EnsureAgentRoots(found.ID); err != nil {
			return err
		}
		source, err = tx.PutAgentSource(&models.AgentKnowledgeSource{
			AgentID: found.ID, Name: "Scenario records", Kind: models.SourceArchive, Enabled: true,
			Specification: models.AgentKnowledgeSpecification{Computer: "scenario", Path: settings.RecordsDirectory, Format: models.FormatRecords},
		})
		return err
	}); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(settings.RecordsDirectory, 0o700); err != nil {
		return nil, err
	}

	report := &ScenarioReport{Scenario: settings.Scenario.Name, StartedAt: time.Now(), Models: scenarioModels(&configuration)}
	progress := settings.Progress
	if progress == nil {
		progress = io.Discard
	}
	for index, step := range settings.Scenario.Steps {
		spent, err := scenarioCost(ctx, settings.Database, found.ID)
		if err != nil {
			return report, err
		}
		if settings.BudgetDollars > 0 && spent > settings.BudgetDollars {
			report.IsStopped = true
			report.StoppedWhy = fmt.Sprintf("spent %.4f of the %.2f budget before step %s", spent, settings.BudgetDollars, step.ID)
			break
		}
		_, _ = fmt.Fprintf(progress, "step %d of %d: %s (%s)\n", index+1, len(settings.Scenario.Steps), step.ID, step.StepKind)
		started := time.Now()
		stepReport := &ScenarioStepReport{ID: step.ID, StepKind: step.StepKind}
		switch step.StepKind {
		case ScenarioStepRecords:
			stepReport.FiledCount, err = worker.fileScenarioRecords(ctx, settings, index, step, owner, found, source)
		case ScenarioStepDream:
			for count := 0; count < step.DreamCount && err == nil; count++ {
				err = worker.dreamScenario(ctx, settings.Database, found.ID)
			}
		case ScenarioStepConversation:
			err = worker.rememberScenarioConversation(ctx, settings.Database, step, found)
		case ScenarioStepCheckpoint:
			stepReport.Questions, err = worker.askScenario(ctx, settings, step, owner, found)
		}
		if err != nil {
			return report, fmt.Errorf("step %s: %w", step.ID, err)
		}
		stepReport.DurationMS = time.Since(started).Milliseconds()
		after, err := scenarioCost(ctx, settings.Database, found.ID)
		if err != nil {
			return report, err
		}
		// Every call a run makes, the answers and their grades included,
		// writes its usage on a message, so the difference is the step's.
		stepReport.Cost = after - spent
		report.TotalCost += stepReport.Cost
		if stepReport.GraphCounts, err = scenarioGraphCounts(ctx, settings.Database, found.ID); err != nil {
			return report, err
		}
		report.Steps = append(report.Steps, stepReport)
	}
	report.FinishedAt = time.Now()
	if len(report.Steps) > 0 {
		report.GraphCounts = report.Steps[len(report.Steps)-1].GraphCounts
	}
	return report, nil
}

// fileScenarioRecords writes a step's records as a file of the folder and
// files what the reader makes of the folder, as a pass of the source
// would: the daemon's reader, then the server's filing.
func (self *Agent) fileScenarioRecords(ctx context.Context, settings *ScenarioSettings, index int, step *ScenarioStep, owner *models.User, found *models.Agent, source *models.AgentKnowledgeSource) (int, error) {
	var content strings.Builder
	for _, record := range step.Records {
		compact, err := json.Marshal(record)
		if err != nil {
			return 0, err
		}
		content.Write(compact)
		content.WriteByte('\n')
	}
	name := filepath.Join(settings.RecordsDirectory, fmt.Sprintf("%03d-%s.jsonl", index+1, step.ID))
	if err := os.WriteFile(name, []byte(content.String()), 0o600); err != nil {
		return 0, err
	}
	run := &Run{Agent: found, Owner: owner, Now: time.Now(), settings: self.settings}
	filed := 0
	after := ""
	for {
		var known map[string]string
		if err := self.settings.Database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
			known, err = tx.ListAgentDocumentHashes(source.ID)
			return err
		}); err != nil {
			return filed, err
		}
		result, err := computer.RunScan(ctx, &computer.Options{Home: settings.RecordsDirectory}, &computer.ScanArguments{
			Root: settings.RecordsDirectory, Format: computer.FormatRecords, Known: known, After: after, Most: ingestRecordEntries,
		})
		if err != nil {
			return filed, err
		}
		for _, entry := range result.Entries {
			if entry.Text != "" {
				filed++
			}
		}
		if _, _, err := self.fileComputerPage(ctx, run, source, ingestPage{Entries: result.Entries, NextCursor: result.Next, IsComplete: result.Next == ""}, nil); err != nil {
			return filed, err
		}
		if result.Next == "" {
			return filed, nil
		}
		after = result.Next
	}
}

// dreamScenario runs one dream through the worker, as `dream now` does.
func (self *Agent) dreamScenario(ctx context.Context, database db.Database, agentId string) error {
	if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
		if _, err := tx.UpdateAgent(agentId, func(agent *models.Agent) error {
			agent.DreamedAt = nil
			return nil
		}); err != nil {
			return err
		}
		_, err := self.Enqueue(tx, models.AgentJobDream, agentId, "", time.Now().Format(time.DateOnly))
		return err
	}); err != nil {
		return err
	}
	if err := self.TickAt(ctx, time.Now()); err != nil {
		return err
	}
	self.Wait()
	return scenarioJobFinished(ctx, database, agentId, models.AgentJobDream, "dream")
}

// scenarioJobFinished says why the run's last job of a kind did not
// finish: one that failed is queued again or dead, and a scenario that
// carried on past it would measure the step that did not happen.
func scenarioJobFinished(ctx context.Context, database db.Database, agentId string, kind models.AgentJobKind, name string) error {
	var failed []*models.AgentJob
	if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		failed, err = tx.ListAgentJobs(&db.AgentJobFilter{
			AgentID: agentId, Kinds: []models.AgentJobKind{kind},
			Statuses: []models.AgentJobStatus{models.AgentJobDead, models.AgentJobQueued},
		}, &db.Options{Limit: 5})
		return err
	}); err != nil {
		return err
	}
	for _, job := range failed {
		return fmt.Errorf("the %s did not finish (%s): %s", name, job.Status, job.Error)
	}
	return nil
}

// askScenario asks a checkpoint's questions.
func (self *Agent) askScenario(ctx context.Context, settings *ScenarioSettings, step *ScenarioStep, owner *models.User, found *models.Agent) ([]*ScenarioQuestionReport, error) {
	var reports []*ScenarioQuestionReport
	for _, question := range step.Questions {
		carried, err := self.RecallForQuestion(ctx, found, owner, question.Question)
		if err != nil {
			return nil, err
		}
		questionReport := &ScenarioQuestionReport{ID: question.ID, IsRecallHit: true}
		for _, claim := range question.Expects {
			if !scenarioCarries(carried, claim) {
				questionReport.IsRecallHit = false
				questionReport.RecallFailure = "did not carry " + describeScenarioClaim(claim)
				break
			}
		}
		if questionReport.IsRecallHit {
			for _, claim := range question.Forbids {
				if scenarioCarries(carried, claim) {
					questionReport.IsRecallHit = false
					questionReport.RecallFailure = "carried " + describeScenarioClaim(claim)
					break
				}
			}
		}
		if questionReport.ExpectedLayers, err = scenarioLayers(ctx, settings.Database, found.ID, question.Expects); err != nil {
			return nil, err
		}
		if questionReport.OutdatedLayers, err = scenarioLayers(ctx, settings.Database, found.ID, question.OutdatedClaims); err != nil {
			return nil, err
		}
		for _, answerFrom := range settings.AnswerSources {
			answerReport := &ScenarioAnswerReport{AnswerFrom: answerFrom}
			var plan *RetrievalPlan
			if strings.HasSuffix(answerFrom, answerFromPlanned) {
				judged, err := self.JudgeRetrievalPlan(ctx, found, owner, question.Question)
				if err != nil {
					return nil, err
				}
				plan = judged.Plan
				if plan == nil || plan.isEmpty() {
					// No plan is what a live turn does with a question
					// like this: the basic search.
					answerFrom = strings.TrimSuffix(answerFrom, answerFromPlanned)
					plan = nil
				} else {
					answerReport.PlannedSearches = plan.Searches
				}
			}
			var evaluation *AnswerEvaluation
			if answerFrom == ScenarioAnswerFromSurvey {
				evaluation, err = self.surveyScenario(ctx, found, owner, question)
			} else {
				evaluation, err = self.EvaluateAnswer(ctx, found, owner, question.Question, question.ExpectedAnswer, question.OutdatedAnswer, answerFrom, plan)
			}
			if err != nil {
				return nil, err
			}
			answerReport.AnswerVerdict = evaluation.AnswerVerdict
			answerReport.VerdictReason = evaluation.VerdictReason
			answerReport.AnswerText = evaluation.AnswerText
			answerReport.Cost = evaluation.Cost
			answerReport.AnswerDurationMS = evaluation.AnswerDurationMS
			questionReport.Answers = append(questionReport.Answers, answerReport)
		}
		questionReport.ShownLessons = self.LessonsForQuestion(ctx, found, owner, question.Question)
		reports = append(reports, questionReport)
	}
	return reports, nil
}

// surveyScenario answers a question with a survey of the whole graph and
// grades the survey's report as the answer.
func (self *Agent) surveyScenario(ctx context.Context, found *models.Agent, owner *models.User, question *ScenarioQuestion) (*AnswerEvaluation, error) {
	started := time.Now()
	surveyed, err := self.Survey(ctx, found, owner, question.Question, "")
	if err != nil {
		return nil, err
	}
	evaluation := &AnswerEvaluation{AnswerText: strings.TrimSpace(surveyed.Report), AnswerDurationMS: time.Since(started).Milliseconds()}
	return self.gradeAnswer(ctx, found, owner, evaluation, question.Question, question.ExpectedAnswer, question.OutdatedAnswer, nil)
}

// rememberScenarioConversation files a conversation of the agent's own
// and runs the remembering a finished conversation gets.
func (self *Agent) rememberScenarioConversation(ctx context.Context, database db.Database, step *ScenarioStep, found *models.Agent) error {
	if err := database.TransactionContext(ctx, func(tx db.Transaction) error {
		conversation, err := tx.CreateAgentConversation(&models.AgentConversation{AgentID: found.ID, Kind: models.AgentConversationNamed, Title: step.ID})
		if err != nil {
			return err
		}
		for _, message := range step.Messages {
			stored := &models.AgentMessage{ConversationID: conversation.ID, Role: message.Role, Content: message.Content, ToolCallID: message.ToolCallID, Name: message.ToolName}
			if message.Role == "tool" {
				stored.Content = fenced(message.Content)
			}
			for _, call := range message.ToolCalls {
				stored.ToolCalls = append(stored.ToolCalls, models.AgentToolCall{ID: call.ID, Name: call.ToolName, Arguments: call.Arguments})
			}
			if _, err := tx.AppendAgentMessage(stored); err != nil {
				return err
			}
		}
		_, err = self.Enqueue(tx, models.AgentJobRemember, found.ID, "", conversation.ID)
		return err
	}); err != nil {
		return err
	}
	if err := self.TickAt(ctx, time.Now()); err != nil {
		return err
	}
	self.Wait()
	return scenarioJobFinished(ctx, database, found.ID, models.AgentJobRemember, "remembering")
}

// scenarioCarries is whether recall carried a claim: every word in one
// fact, summary or overview section of the page named, or of a page under
// it; any page when the claim names none.
func scenarioCarries(carried []*RecalledPage, claim *ScenarioClaim) bool {
	for _, page := range carried {
		if claim.Path != "" && !strings.EqualFold(page.Path, claim.Path) && !strings.HasPrefix(strings.ToLower(page.Path), strings.ToLower(claim.Path)+"/") {
			continue
		}
		if len(claim.Words) == 0 {
			return true
		}
		for _, fact := range page.Facts {
			if scenarioSays(fact.Text, claim.Words) {
				return true
			}
		}
		if scenarioSays(page.Summary, claim.Words) || scenarioSays(page.Overview, claim.Words) {
			return true
		}
	}
	return false
}

func scenarioSays(text string, words []string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	lowered := strings.ToLower(text)
	for _, word := range words {
		if !strings.Contains(lowered, strings.ToLower(strings.TrimSpace(word))) {
			return false
		}
	}
	return true
}

func describeScenarioClaim(claim *ScenarioClaim) string {
	where := "any page"
	if claim.Path != "" {
		where = claim.Path
	}
	return where + " saying \"" + strings.Join(claim.Words, " ") + "\""
}

// scenarioLayers counts, for each claim, the places in the graph that say
// it: facts current and not, page summaries and overviews, themes,
// reflections, lessons, and the documents they were read from.
func scenarioLayers(ctx context.Context, database db.Database, agentId string, claims []*ScenarioClaim) ([]*ScenarioClaimLayers, error) {
	if len(claims) == 0 {
		return []*ScenarioClaimLayers{}, nil
	}
	var nodes []*models.AgentNode
	var facts []*models.AgentFact
	var documents map[string][]string
	if err := database.TransactionContext(ctx, func(tx db.Transaction) (err error) {
		if nodes, err = tx.ListAgentNodesUnder(agentId, "", 100000); err != nil {
			return err
		}
		if facts, err = tx.ListAgentFactsLearnedSince(agentId, time.Time{}, 100000); err != nil {
			return err
		}
		documents, err = tx.ListAgentChunkTexts(agentId, 100000)
		return err
	}); err != nil {
		return nil, err
	}
	pathOf := map[string]string{}
	for _, node := range nodes {
		pathOf[node.ID] = node.Path
	}
	isTheme := func(path string) bool {
		return path == models.PathThemes || strings.HasPrefix(path, models.PathThemes+"/")
	}
	var layers []*ScenarioClaimLayers
	for _, claim := range claims {
		counts := map[string]int{}
		reaches := func(path string) bool {
			return claim.Path == "" || strings.EqualFold(path, claim.Path) || strings.HasPrefix(strings.ToLower(path), strings.ToLower(claim.Path)+"/")
		}
		for _, fact := range facts {
			path := pathOf[fact.NodeID]
			if !reaches(path) && !isTheme(path) || !scenarioSays(fact.Text, claim.Words) {
				continue
			}
			switch {
			case fact.Kind == models.FactReflection:
				counts[scenarioLayerReflection]++
			case fact.Kind == models.FactLesson:
				counts[scenarioLayerLesson]++
			case isTheme(path):
				counts[scenarioLayerTheme]++
			case fact.SupersededBy != "" || fact.Dormant:
				counts[scenarioLayerOutdated]++
			default:
				counts[scenarioLayerFact]++
			}
		}
		for _, node := range nodes {
			if isTheme(node.Path) {
				if scenarioSays(node.Summary, claim.Words) || scenarioSays(node.Overview, claim.Words) {
					counts[scenarioLayerTheme]++
				}
				continue
			}
			if !reaches(node.Path) {
				continue
			}
			if scenarioSays(node.Summary, claim.Words) {
				counts[scenarioLayerSummary]++
			}
			if scenarioSays(node.Overview, claim.Words) {
				counts[scenarioLayerOverview]++
			}
		}
		// The documents answer "was it ever said", whatever the page.
		for _, chunks := range documents {
			for _, chunk := range chunks {
				if scenarioSays(chunk, claim.Words) {
					counts[scenarioLayerDocument]++
					break
				}
			}
		}
		layers = append(layers, &ScenarioClaimLayers{Claim: describeScenarioClaim(claim), LayerCounts: counts})
	}
	return layers, nil
}

// scenarioCost is what the run's calls have recorded as spent so far.
func scenarioCost(ctx context.Context, database db.Database, agentId string) (cost float64, err error) {
	err = database.TransactionContext(ctx, func(tx db.Transaction) error {
		cost, err = tx.SumAgentMessageCost(agentId)
		return err
	})
	return cost, err
}

func scenarioGraphCounts(ctx context.Context, database db.Database, agentId string) (*ScenarioGraphCounts, error) {
	counts := &ScenarioGraphCounts{}
	err := database.TransactionContext(ctx, func(tx db.Transaction) error {
		nodes, err := tx.ListAgentNodesUnder(agentId, "", 100000)
		if err != nil {
			return err
		}
		counts.PageCount = len(nodes)
		for _, node := range nodes {
			if strings.TrimSpace(node.Overview) != "" {
				counts.OverviewCount++
			}
		}
		facts, err := tx.ListAgentFactsLearnedSince(agentId, time.Time{}, 100000)
		if err != nil {
			return err
		}
		counts.FactCount = len(facts)
		documents, err := tx.ListAgentChunkTexts(agentId, 100000)
		counts.DocumentCount = len(documents)
		return err
	})
	return counts, err
}

// scenarioModels is the models the run used, for the report.
func scenarioModels(configuration *config.Configuration) map[string]string {
	named := configuration.Agent.Models
	return map[string]string{
		"default": named.Default, "fast": named.Fast, "scan": named.Scan, "synthesize": named.Synthesize,
		"research": named.Research, "embedding": named.Embedding,
	}
}
