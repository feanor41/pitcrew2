package cli

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/fmazzalomo/pitcrew/internal/activity"
	"github.com/fmazzalomo/pitcrew/internal/agentbrief"
	"github.com/fmazzalomo/pitcrew/internal/consolidate"
	"github.com/fmazzalomo/pitcrew/internal/correction"
	"github.com/fmazzalomo/pitcrew/internal/delivery"
	"github.com/fmazzalomo/pitcrew/internal/envelope"
	"github.com/fmazzalomo/pitcrew/internal/evidence"
	"github.com/fmazzalomo/pitcrew/internal/handles"
	"github.com/fmazzalomo/pitcrew/internal/history"
	"github.com/fmazzalomo/pitcrew/internal/maxims"
	"github.com/fmazzalomo/pitcrew/internal/plan"
	"github.com/fmazzalomo/pitcrew/internal/project"
	"github.com/fmazzalomo/pitcrew/internal/runtimeinstall"
	"github.com/fmazzalomo/pitcrew/internal/store"
	"github.com/fmazzalomo/pitcrew/internal/tui"
	"github.com/fmazzalomo/pitcrew/internal/version"
	"github.com/fmazzalomo/pitcrew/internal/workflow"
)

const helpEpilogue = "Read the four maxims of the harness: pitcrew principles."

var (
	ErrUsage = errors.New("invalid command usage")
	ErrState = errors.New("invalid command state")
)

type Dependencies struct {
	Stdin         io.Reader
	Stdout        io.Writer
	Stderr        io.Writer
	ProjectRoot   string
	DataHome      string
	Now           func() time.Time
	Entropy       io.Reader
	TUIRunner     func(string, io.Reader, io.Writer) error
	InstallRunner func(string, runtimeinstall.Dependencies) int
}

type stageArtifactInput struct {
	Content       string                     `json:"content"`
	SchemaVersion *int                       `json:"schema_version,omitempty"`
	Entries       *[]workflow.NormativeEntry `json:"entries,omitempty"`
}
type progressInput struct {
	Status     string `json:"status"`
	Summary    string `json:"summary"`
	NextAction string `json:"next_action"`
}
type capabilityRequestInput struct {
	Capability    string `json:"capability"`
	Reason        string `json:"reason"`
	BlockedAction string `json:"blocked_action"`
}
type reviewInput struct {
	Verdict    *evidence.Verdict    `json:"verdict"`
	Summary    *string              `json:"summary"`
	Findings   *string              `json:"findings"`
	PlanImpact *evidence.PlanImpact `json:"plan_impact,omitempty"`
}
type aggregateReviewInput struct {
	Verdict          *evidence.Verdict            `json:"verdict"`
	Summary          *string                      `json:"summary"`
	Findings         *string                      `json:"findings"`
	VerificationRuns []evidence.VerificationRun   `json:"verification_runs,omitempty"`
	Checkpoint       *evidence.ReviewedCheckpoint `json:"checkpoint,omitempty"`
}

func Run(args []string, deps Dependencies) int {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Entropy == nil {
		deps.Entropy = rand.Reader
	}
	if len(args) == 0 || equalArgs(args, "--help") {
		writeHelp(deps.Stdout, rootHelp)
		return int(envelope.OK)
	}
	if equalArgs(args, "--version") {
		fmt.Fprintf(deps.Stdout, "pitcrew %s\n", version.Current)
		return int(envelope.OK)
	}
	switch args[0] {
	case "agent":
		return runAgent(args[1:], deps)
	case "install":
		return runInstall(args[1:], deps)
	case "tui":
		if len(args) != 1 {
			return fail(deps, ErrUsage, "usage: pitcrew tui")
		}
		return runTUI(deps)
	case "principles":
		return runPrinciples(args[1:], deps)
	case "project":
		return runProject(args[1:], deps)
	case "context":
		return runContext(args[1:], deps)
	case "delivery":
		return runDelivery(args[1:], deps)
	case "workflow":
		return runWorkflow(args[1:], deps)
	default:
		return fail(deps, ErrUsage, fmt.Sprintf("unknown command %q", args[0]))
	}
}

func runAgent(args []string, deps Dependencies) int {
	if equalArgs(args, "--help") || equalArgs(args, "brief", "--help") {
		writeHelp(deps.Stdout, "Usage: pitcrew agent brief --role <role> [--workflow-id <id>] [--unit-id <id>] [--json]\n")
		return int(envelope.OK)
	}
	if len(args) == 0 || args[0] != "brief" {
		return fail(deps, ErrUsage, "usage: pitcrew agent brief --role <role> [--workflow-id <id>] [--unit-id <id>] [--json]")
	}
	values, err := parseFlags(args[1:], flagRules{required: []string{"--role"}, optional: []string{"--workflow-id", "--unit-id"}, boolean: []string{"--json"}})
	if err != nil {
		return fail(deps, err, err.Error())
	}
	brief, err := agentbrief.New(values.one("--role"), values.one("--workflow-id"), values.one("--unit-id"))
	if err != nil {
		return fail(deps, ErrUsage, err.Error())
	}
	jsonOutput := values.one("--json") != ""
	if brief.Contract.Role == "aion" && values.one("--workflow-id") == "" {
		write := func(continuity history.ActiveContinuity) error {
			return writeAgentBrief(deps, brief.WithContinuity(continuity), jsonOutput)
		}
		return withReadStoreOrUninitialized(deps, func(s *store.Store) error {
			continuity, err := history.New(s).ActiveContinuity(context.Background())
			if err != nil {
				return err
			}
			return write(continuity)
		}, func() error { return write(history.EmptyActiveContinuity()) })
	}
	if brief.Contract.Role == "aion" {
		return withReadStore(deps, func(s *store.Store) error {
			projection, err := history.New(s).Project(context.Background(), values.one("--workflow-id"), history.ViewCoordination, "")
			if err != nil {
				return err
			}
			return writeAgentBrief(deps, brief.WithCoordination(projection), jsonOutput)
		})
	}
	if brief.Contract.Role != "daimon" && brief.Contract.Role != "aion" && brief.Contract.Role != "pc2-sdd-initializer" {
		return withReadStore(deps, func(s *store.Store) error {
			svc, ctx := history.New(s), context.Background()
			workflowID, unitID := values.one("--workflow-id"), values.one("--unit-id")
			switch brief.Contract.Role {
			case "pc2-explorer", "pc2-specifier", "pc2-designer", "pc2-task-planner":
				projection, err := svc.Project(ctx, workflowID, history.ViewPhase, "")
				if err != nil {
					return err
				}
				brief = brief.WithPhase(projection)
			case "pc2-implementer", "pc2-reviewer":
				if brief.Contract.Role == "pc2-reviewer" && unitID == "" {
					projection, err := svc.Project(ctx, workflowID, history.ViewAggregate, "")
					if err != nil {
						return err
					}
					brief = brief.WithAggregate(projection)
					break
				}
				unit, err := svc.Project(ctx, workflowID, history.ViewUnit, unitID)
				if err != nil {
					return err
				}
				coordination, err := svc.Project(ctx, workflowID, history.ViewCoordination, "")
				if err != nil {
					return err
				}
				aggregate, err := svc.Project(ctx, workflowID, history.ViewAggregate, "")
				if err != nil {
					return err
				}
				brief = brief.WithUnit(unit, coordination, aggregate, brief.Contract.Role == "pc2-reviewer")
			}
			return writeAgentBrief(deps, brief, jsonOutput)
		})
	}
	if err = writeAgentBrief(deps, brief, jsonOutput); err != nil {
		return fail(deps, err, err.Error())
	}
	return int(envelope.OK)
}

func writeAgentBrief(deps Dependencies, brief agentbrief.Brief, jsonOutput bool) error {
	if jsonOutput {
		return writeSuccess(deps, map[string]any{"brief": brief}, brief.NextAction)
	}
	return agentbrief.WriteText(deps.Stdout, brief)
}

func runInstall(args []string, deps Dependencies) int {
	if equalArgs(args, "--help") {
		writeHelp(deps.Stdout, installHelp)
		return int(envelope.OK)
	}
	if len(args) != 1 || !isInstallTarget(args[0]) {
		return fail(deps, ErrUsage, "usage: pitcrew install <codex|opencode|claude|pi>")
	}
	runner := deps.InstallRunner
	if runner == nil {
		runner = runtimeinstall.Run
	}
	return runner(args[0], runtimeinstall.Dependencies{
		Stdin: deps.Stdin, Stdout: deps.Stdout, Stderr: deps.Stderr, Cwd: deps.ProjectRoot,
	})
}

func isInstallTarget(target string) bool {
	switch target {
	case "codex", "opencode", "claude", "pi":
		return true
	default:
		return false
	}
}

func runTUI(deps Dependencies) int {
	runner := deps.TUIRunner
	if deps.DataHome != "" {
		inspection, err := project.Inspect(deps.ProjectRoot, deps.DataHome)
		if err != nil {
			return fail(deps, err, err.Error())
		}
		if runner != nil {
			if err := runner(inspection.Paths.ProjectRoot, deps.Stdin, deps.Stdout); err != nil {
				return fail(deps, err, err.Error())
			}
			return int(envelope.OK)
		}
		return runProjectTUI(deps, inspection)
	}
	if runner == nil {
		runner = runEmbeddedTUI
	}
	if err := runner(deps.ProjectRoot, deps.Stdin, deps.Stdout); err != nil {
		return fail(deps, err, err.Error())
	}
	return int(envelope.OK)
}

func runProjectTUI(deps Dependencies, inspection project.Inspection) int {
	opened, err := readProjectStore(inspection)
	loader := tui.Loader(failedHistory{err})
	if err == nil {
		defer opened.Close()
		loader = history.New(opened)
	}
	if err = tui.Run(tui.New(loader), deps.Stdin, deps.Stdout); err != nil {
		return fail(deps, err, err.Error())
	}
	return int(envelope.OK)
}

func runProject(args []string, deps Dependencies) int {
	if equalArgs(args, "--help") {
		writeHelp(deps.Stdout, "Usage: pitcrew project <inspect|consolidate> [options]\n")
		return 0
	}
	if len(args) == 2 && args[1] == "--help" && (args[0] == "inspect" || args[0] == "consolidate") {
		writeHelp(deps.Stdout, "Usage: pitcrew project "+args[0]+" [options]\n")
		return 0
	}
	if len(args) == 1 && args[0] == "inspect" {
		inspection, err := project.Inspect(deps.ProjectRoot, deps.DataHome)
		if err != nil {
			return fail(deps, err, err.Error())
		}
		next := "workflow new"
		if len(inspection.Legacy.Candidates) != 0 {
			next = "project consolidate"
		}
		data := map[string]any{"project_id": inspection.Project.ID, "git_common_dir": inspection.Project.CommonDir, "checkout_root": inspection.Project.CheckoutRoot, "initialized": inspection.Initialized, "repository_move_boundary": inspection.RepositoryMoveBoundary, "paths": map[string]string{"project_root": inspection.Paths.ProjectRoot, "state_path": inspection.Paths.StatePath, "worktree_root": inspection.Paths.WorktreeRoot, "handle_root": inspection.Paths.HandleRoot}, "legacy": inspection.Legacy}
		if err = writeSuccess(deps, data, next); err != nil {
			return fail(deps, err, err.Error())
		}
		return 0
	}
	if len(args) >= 1 && args[0] == "consolidate" {
		values, err := parseFlags(args[1:], flagRules{required: []string{"--input-file"}})
		if err != nil {
			return fail(deps, err, err.Error())
		}
		manifest, err := decodeInputFile[consolidate.Manifest](values.one("--input-file"))
		if err != nil {
			return fail(deps, err, err.Error())
		}
		inspection, err := project.Inspect(deps.ProjectRoot, deps.DataHome)
		if err != nil {
			return fail(deps, err, err.Error())
		}
		destination, err := store.OpenProject(context.Background(), inspection.Project, inspection.Paths)
		if err == nil {
			defer destination.Close()
			err = (consolidate.Service{}).Consolidate(context.Background(), destination.DB(), inspection.Project, manifest)
		}
		if err != nil {
			return fail(deps, err, err.Error())
		}
		if err = writeSuccess(deps, map[string]any{"project_id": inspection.Project.ID, "candidate_set_id": inspection.Legacy.CandidateSetID}, "workflow show"); err != nil {
			return fail(deps, err, err.Error())
		}
		return 0
	}
	return fail(deps, ErrUsage, "usage: pitcrew project <inspect|consolidate>")
}

func runEmbeddedTUI(root string, input io.Reader, output io.Writer) error {
	opened, err := store.OpenReadOnly(context.Background(), root)
	if err != nil {
		return tui.Run(tui.New(failedHistory{err}), input, output)
	}
	if opened.State == store.Uninitialized {
		return tui.Run(tui.New(failedHistory{tui.ErrUninitialized}), input, output)
	}
	defer opened.Store.Close()
	return tui.Run(tui.New(history.New(opened.Store)), input, output)
}

type failedHistory struct{ err error }

func (f failedHistory) List(context.Context) ([]history.Workflow, error) { return nil, f.err }
func (f failedHistory) Detail(context.Context, string) (history.Detail, error) {
	return history.Detail{}, f.err
}
func (f failedHistory) Search(context.Context, string) ([]history.SearchResult, error) {
	return nil, f.err
}
func (f failedHistory) Resolve(context.Context, history.SearchResult) (history.Resolution, error) {
	return history.Resolution{}, f.err
}
func (f failedHistory) ResolveActivity(context.Context, history.Activity) (history.Resolution, error) {
	return history.Resolution{}, f.err
}

func runPrinciples(args []string, deps Dependencies) int {
	if len(args) == 0 {
		if _, err := io.WriteString(deps.Stdout, maxims.Text()); err != nil {
			return fail(deps, err, err.Error())
		}
		return 0
	}
	if equalArgs(args, "--help") {
		writeHelp(deps.Stdout, principlesHelp)
		return 0
	}
	if !equalArgs(args, "--json") {
		return fail(deps, ErrUsage, "usage: pitcrew principles [--json]")
	}
	items, err := maxims.Structured()
	if err != nil {
		return fail(deps, err, err.Error())
	}
	if err = json.NewEncoder(deps.Stdout).Encode(items); err != nil {
		return fail(deps, err, err.Error())
	}
	return 0
}

var workflowCommands = map[string]bool{"new": true, "continue": true, "show": true, "progress": true, "request-capability": true, "explore": true, "spec": true, "design": true, "plan": true, "amend-plan": true, "approve-plan": true, "list-ready-units": true, "begin-implementation": true, "complete": true, "authorize-correction": true, "abandon": true, "claim-unit": true, "recover-unit-claim": true, "recover-aggregate": true, "handoff-review": true, "recover-review": true, "unit-tdd": true, "unit-review": true, "unit-complete": true}
var workflowIDPattern = regexp.MustCompile(`^wf-[0-9a-f]{24}$`)
var deliveryIDPattern = regexp.MustCompile(`^(dl|wf)-[0-9a-f]{24}$`)

func runDelivery(args []string, deps Dependencies) int {
	if equalArgs(args, "--help") {
		writeHelp(deps.Stdout, deliveryHelp)
		return 0
	}
	if len(args) == 0 {
		return fail(deps, ErrUsage, "delivery subcommand is required")
	}
	command, rest := args[0], args[1:]
	if equalArgs(rest, "--help") {
		if command == "start" || command == "update" || command == "show" || command == "search" || command == "active" {
			writeHelp(deps.Stdout, "Usage: pitcrew delivery "+command+" [options]\n")
			return 0
		}
	}
	switch command {
	case "start":
		return runDeliveryStart(rest, deps)
	case "update":
		return runDeliveryUpdate(rest, deps)
	case "show":
		return runDeliveryShow(rest, deps)
	case "search":
		return runDeliverySearch(rest, deps)
	case "active":
		return runDeliveryActive(rest, deps)
	default:
		return fail(deps, ErrUsage, fmt.Sprintf("unknown delivery subcommand %q", command))
	}
}

func runDeliveryStart(args []string, deps Dependencies) int {
	values, err := parseFlags(args, flagRules{required: []string{"--actor", "--input-file"}})
	if err != nil {
		return fail(deps, err, err.Error())
	}
	input, err := decodeInputFile[delivery.StartInput](values.one("--input-file"))
	if err != nil {
		return fail(deps, err, err.Error())
	}
	return withStore(deps, func(s *store.Store) error {
		trace, err := delivery.NewService(s, deps.Now).Start(context.Background(), values.one("--actor"), input)
		if err != nil {
			return deliveryStateError(err)
		}
		return writeSuccess(deps, map[string]any{"delivery": trace}, "delivery show")
	})
}

func runDeliveryUpdate(args []string, deps Dependencies) int {
	values, err := parseFlags(args, flagRules{required: []string{"--delivery-id", "--revision", "--actor", "--input-file"}})
	if err != nil {
		return fail(deps, err, err.Error())
	}
	if !strings.HasPrefix(values.one("--delivery-id"), "dl-") || !deliveryIDPattern.MatchString(values.one("--delivery-id")) {
		return fail(deps, ErrUsage, "--delivery-id must be a direct delivery ID")
	}
	revision, err := values.int64("--revision")
	if err != nil {
		return fail(deps, err, err.Error())
	}
	input, err := decodeInputFile[delivery.UpdateInput](values.one("--input-file"))
	if err != nil {
		return fail(deps, err, err.Error())
	}
	return withStore(deps, func(s *store.Store) error {
		trace, err := delivery.NewService(s, deps.Now).Update(context.Background(), values.one("--delivery-id"), revision, values.one("--actor"), input)
		if err != nil {
			return deliveryStateError(err)
		}
		return writeSuccess(deps, map[string]any{"delivery": trace}, trace.NextAction)
	})
}

func runDeliveryShow(args []string, deps Dependencies) int {
	values, err := parseFlags(args, flagRules{required: []string{"--delivery-id"}})
	if err != nil {
		return fail(deps, err, err.Error())
	}
	if !deliveryIDPattern.MatchString(values.one("--delivery-id")) {
		return fail(deps, ErrUsage, "--delivery-id must be a delivery or workflow ID")
	}
	return withReadStore(deps, func(s *store.Store) error {
		detail, err := history.New(s).GetDelivery(context.Background(), values.one("--delivery-id"))
		if err != nil {
			return err
		}
		return writeSuccess(deps, detail, detail.Delivery.NextAction)
	})
}

func runDeliverySearch(args []string, deps Dependencies) int {
	values, err := parseFlags(args, flagRules{required: []string{"--query"}})
	if err != nil {
		return fail(deps, err, err.Error())
	}
	if strings.TrimSpace(values.one("--query")) == "" {
		return fail(deps, ErrUsage, "--query must be nonblank")
	}
	return withReadStore(deps, func(s *store.Store) error {
		results, err := history.New(s).SearchDeliveries(context.Background(), values.one("--query"))
		if err != nil {
			return err
		}
		return writeSuccess(deps, map[string]any{"deliveries": results}, "delivery show")
	})
}

func runDeliveryActive(args []string, deps Dependencies) int {
	if len(args) != 0 {
		return fail(deps, ErrUsage, "delivery active accepts no options")
	}
	write := func(continuity history.ActiveContinuity) error {
		return writeSuccess(deps, map[string]any{"deliveries": continuity.Deliveries}, continuity.NextAction)
	}
	return withReadStoreOrUninitialized(deps, func(s *store.Store) error {
		continuity, err := history.New(s).ActiveContinuity(context.Background())
		if err != nil {
			return err
		}
		return write(continuity)
	}, func() error { return write(history.EmptyActiveContinuity()) })
}

func deliveryStateError(err error) error {
	if errors.Is(err, store.ErrCASMismatch) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrState, err)
}

func runWorkflow(args []string, deps Dependencies) int {
	if equalArgs(args, "--help") {
		writeHelp(deps.Stdout, workflowHelp)
		return 0
	}
	if len(args) == 0 {
		return fail(deps, ErrUsage, "workflow subcommand is required")
	}
	command, rest := args[0], args[1:]
	if !workflowCommands[command] {
		return fail(deps, ErrUsage, fmt.Sprintf("unknown workflow subcommand %q", command))
	}
	if equalArgs(rest, "--help") {
		writeHelp(deps.Stdout, "Usage: pitcrew workflow "+command+" [options]\n")
		return 0
	}
	switch command {
	case "new":
		return runWorkflowNew(rest, deps)
	case "continue":
		return runWorkflowContinue(rest, deps)
	case "show":
		return runWorkflowShow(rest, deps)
	case "progress":
		return runProgress(rest, deps)
	case "request-capability":
		return runRequestCapability(rest, deps)
	case "explore", "spec", "design":
		return runStage(command, rest, deps)
	case "plan":
		return runPlan(rest, deps)
	case "amend-plan":
		return runAmendPlan(rest, deps)
	case "approve-plan":
		return runApprovePlan(rest, deps)
	case "list-ready-units":
		return runReady(rest, deps)
	case "begin-implementation":
		return runBeginImplementation(rest, deps)
	case "complete":
		return runComplete(rest, deps)
	case "authorize-correction":
		return runAuthorizeCorrection(rest, deps)
	case "abandon":
		return runAbandon(rest, deps)
	case "recover-aggregate":
		return runRecoverAggregate(rest, deps)
	case "claim-unit", "recover-unit-claim", "recover-review":
		return runClaim(command, rest, deps)
	case "handoff-review":
		return runHandoffReview(rest, deps)
	case "unit-tdd":
		return runUnitTDD(rest, deps)
	case "unit-review":
		return runUnitReview(rest, deps)
	case "unit-complete":
		return runUnitComplete(rest, deps)
	default:
		return fail(deps, ErrUsage, "unsupported command")
	}
}

func runRequestCapability(args []string, deps Dependencies) int {
	values, err := parseFlags(args, flagRules{required: []string{"--workflow-id", "--revision", "--actor", "--input-file"}})
	if err != nil {
		return fail(deps, err, err.Error())
	}
	revision, err := values.int64("--revision")
	if err != nil {
		return fail(deps, err, err.Error())
	}
	input, err := decodeInputFile[capabilityRequestInput](values.one("--input-file"))
	if err != nil {
		return fail(deps, err, err.Error())
	}
	input.Capability, input.Reason, input.BlockedAction = strings.TrimSpace(input.Capability), strings.TrimSpace(input.Reason), strings.TrimSpace(input.BlockedAction)
	if input.Capability == "" || input.Reason == "" || input.BlockedAction == "" {
		return fail(deps, ErrState, "capability request requires capability, reason, and blocked_action")
	}
	content, err := json.Marshal(input)
	if err != nil {
		return fail(deps, err, err.Error())
	}
	return withStore(deps, func(s *store.Store) error {
		_, err := workflow.New(s, deps.Now).AppendOperational(context.Background(), values.one("--workflow-id"), revision, "capability_request", string(content), values.one("--actor"), activity.CapabilityRequested)
		if err != nil {
			return err
		}
		return writeSuccess(deps, map[string]any{"capability_request": input}, "aion coordinate requested capability")
	})
}

func runProgress(args []string, deps Dependencies) int {
	values, err := parseFlags(args, flagRules{required: []string{"--workflow-id", "--revision", "--actor", "--input-file"}})
	if err != nil {
		return fail(deps, err, err.Error())
	}
	revision, err := values.int64("--revision")
	if err != nil {
		return fail(deps, err, err.Error())
	}
	input, err := decodeInputFile[progressInput](values.one("--input-file"))
	if err != nil {
		return fail(deps, err, err.Error())
	}
	input.Summary, input.NextAction = strings.TrimSpace(input.Summary), strings.TrimSpace(input.NextAction)
	if (input.Status != "advanced" && input.Status != "blocked") || input.Summary == "" || input.NextAction == "" {
		return fail(deps, ErrState, "progress requires status advanced or blocked, summary, and next_action")
	}
	content, err := json.Marshal(input)
	if err != nil {
		return fail(deps, err, err.Error())
	}
	return withStore(deps, func(s *store.Store) error {
		_, err := workflow.New(s, deps.Now).AppendOperational(context.Background(), values.one("--workflow-id"), revision, "progress", string(content), values.one("--actor"), activity.ProgressRecorded)
		if err != nil {
			return err
		}
		return writeSuccess(deps, map[string]any{"progress": input}, input.NextAction)
	})
}

func runWorkflowContinue(args []string, deps Dependencies) int {
	values, err := parseFlags(args, flagRules{required: []string{"--from", "--actor"}})
	if err != nil {
		return fail(deps, err, err.Error())
	}
	if !workflowIDPattern.MatchString(values.one("--from")) {
		return fail(deps, ErrUsage, "--from must be a workflow ID")
	}
	return withStore(deps, func(s *store.Store) error {
		result, err := workflow.New(s, deps.Now).Continue(context.Background(), values.one("--from"), values.one("--actor"))
		if err != nil {
			return err
		}
		return writeSuccess(deps, result, "workflow explore")
	})
}

func runHandoffReview(args []string, deps Dependencies) int {
	values, err := parseFlags(args, flagRules{required: []string{"--workflow-id", "--unit-id", "--revision", "--actor", "--handle-dir"}})
	if err != nil {
		return fail(deps, err, err.Error())
	}
	revision, err := values.int64("--revision")
	if err != nil {
		return fail(deps, err, err.Error())
	}
	return withStore(deps, func(s *store.Store) error {
		result, err := handles.New(s, deps.Now, deps.Entropy).HandoffReviewAt(context.Background(), values.one("--workflow-id"), values.one("--unit-id"), revision, values.one("--actor"), values.one("--handle-dir"))
		if err != nil {
			return err
		}
		return writeSuccess(deps, map[string]any{"handle_path": result.Path}, "workflow unit-review")
	})
}

func runWorkflowNew(args []string, deps Dependencies) int {
	values, err := parseFlags(args, flagRules{required: []string{"--name", "--goal", "--actor"}})
	if err != nil {
		return fail(deps, err, err.Error())
	}
	name, err := workflow.NormalizeName(values.one("--name"))
	if err != nil {
		return fail(deps, err, err.Error())
	}
	return withStore(deps, func(s *store.Store) error {
		created, err := workflow.New(s, deps.Now).Create(context.Background(), name, values.one("--goal"), values.one("--actor"))
		if err != nil {
			return err
		}
		return writeSuccess(deps, map[string]any{"workflow": created}, "workflow explore")
	})
}
func runWorkflowShow(args []string, deps Dependencies) int {
	values, err := parseFlags(args, flagRules{required: []string{"--workflow-id"}, optional: []string{"--view", "--unit-id"}})
	if err != nil {
		return fail(deps, err, err.Error())
	}
	view, unitID, explicitView, err := workflowShowView(values)
	if err != nil {
		return fail(deps, err, err.Error())
	}
	return withReadStore(deps, func(s *store.Store) error {
		if explicitView && view != history.ViewAudit {
			projected, err := history.New(s).Project(context.Background(), values.one("--workflow-id"), view, unitID)
			if err != nil {
				return err
			}
			next := workflow.NextAction(workflow.State(projected.Workflow.State))
			if projected.Coordination != nil {
				next = projected.Coordination.NextAction
			}
			return writeSuccess(deps, projected, next)
		}
		svc := workflow.New(s, deps.Now)
		current, err := svc.Get(context.Background(), values.one("--workflow-id"))
		if err != nil {
			return err
		}
		artifacts, err := svc.Artifacts(context.Background(), current.ID)
		if err != nil {
			return err
		}
		detail, err := history.New(s).Detail(context.Background(), current.ID)
		if err != nil {
			return err
		}
		return writeSuccess(deps, map[string]any{"workflow": current, "synopsis": detail.Synopsis, "artifacts": artifacts, "records": detail.Records, "timeline": detail.Timeline}, detail.Synopsis.NextAction)
	})
}

func workflowShowView(values flagValues) (history.View, string, bool, error) {
	viewValue, unitID := values.one("--view"), values.one("--unit-id")
	if viewValue == "" {
		if unitID != "" {
			return "", "", false, fmt.Errorf("%w: --unit-id requires --view unit", ErrUsage)
		}
		return history.ViewAudit, "", false, nil
	}
	view := history.View(viewValue)
	switch view {
	case history.ViewCoordination, history.ViewPhase, history.ViewAggregate, history.ViewAudit:
		if unitID != "" {
			return "", "", true, fmt.Errorf("%w: --unit-id is only valid with --view unit", ErrUsage)
		}
	case history.ViewUnit:
		if unitID == "" {
			return "", "", true, fmt.Errorf("%w: --view unit requires --unit-id", ErrUsage)
		}
	default:
		return "", "", true, fmt.Errorf("%w: --view must be coordination, phase, unit, aggregate, or audit", ErrUsage)
	}
	return view, unitID, true, nil
}
func runStage(command string, args []string, deps Dependencies) int {
	values, revision, input, err := workflowInput(args)
	if err != nil {
		return fail(deps, err, err.Error())
	}
	kind := map[string]string{"explore": "exploration", "spec": "specification", "design": "design"}[command]
	event := map[string]workflow.EventType{"explore": workflow.Explore, "spec": workflow.Specify, "design": workflow.Design}[command]
	if strings.TrimSpace(input.Content) == "" {
		return fail(deps, ErrState, "artifact content is required")
	}
	typed, err := input.normative()
	if err != nil {
		return fail(deps, err, err.Error())
	}
	return withStore(deps, func(s *store.Store) error {
		svc := workflow.New(s, deps.Now)
		var current workflow.Workflow
		if typed == nil {
			current, err = svc.RecordArtifact(context.Background(), values.one("--workflow-id"), revision, event, kind, input.Content, values.one("--actor"))
		} else {
			current, err = svc.RecordNormativeArtifact(context.Background(), values.one("--workflow-id"), revision, event, kind, *typed, values.one("--actor"))
		}
		if err != nil {
			return err
		}
		return writeSuccess(deps, map[string]any{"workflow": current}, nextAction(current.State))
	})
}

func (input stageArtifactInput) normative() (*workflow.NormativeArtifact, error) {
	if input.SchemaVersion == nil && input.Entries == nil {
		return nil, nil
	}
	if input.SchemaVersion == nil || input.Entries == nil {
		return nil, fmt.Errorf("%w: schema_version and entries must be supplied together", ErrUsage)
	}
	if *input.SchemaVersion != 1 {
		return nil, fmt.Errorf("%w: schema_version must be 1", ErrUsage)
	}
	return &workflow.NormativeArtifact{Content: input.Content, SchemaVersion: *input.SchemaVersion, Entries: *input.Entries}, nil
}
func workflowInput(args []string) (flagValues, int64, stageArtifactInput, error) {
	values, err := parseFlags(args, flagRules{required: []string{"--workflow-id", "--revision", "--actor", "--input-file"}})
	if err != nil {
		return nil, 0, stageArtifactInput{}, err
	}
	revision, err := values.int64("--revision")
	if err != nil {
		return nil, 0, stageArtifactInput{}, err
	}
	input, err := decodeInputFile[stageArtifactInput](values.one("--input-file"))
	return values, revision, input, err
}

func runPlan(args []string, deps Dependencies) int {
	values, err := parseFlags(args, flagRules{required: []string{"--workflow-id", "--revision", "--actor", "--input-file"}})
	if err != nil {
		return fail(deps, err, err.Error())
	}
	revision, err := values.int64("--revision")
	if err != nil {
		return fail(deps, err, err.Error())
	}
	input, err := decodeInputFile[plan.Plan](values.one("--input-file"))
	if err != nil {
		return fail(deps, err, err.Error())
	}
	if err = plan.Validate(input); err != nil {
		return fail(deps, ErrState, err.Error())
	}
	return withStore(deps, func(s *store.Store) error {
		current, err := plan.NewService(s, deps.Now).Submit(context.Background(), values.one("--workflow-id"), revision, values.one("--actor"), input)
		if err != nil {
			return err
		}
		return writeSuccess(deps, map[string]any{"workflow": current, "plan": input}, "workflow approve-plan")
	})
}
func runAmendPlan(args []string, deps Dependencies) int {
	values, err := parseFlags(args, flagRules{required: []string{"--workflow-id", "--revision", "--actor", "--input-file"}})
	if err != nil {
		return fail(deps, err, err.Error())
	}
	if _, err = values.int64("--revision"); err != nil {
		return fail(deps, err, err.Error())
	}
	input, err := decodeInputFile[plan.Plan](values.one("--input-file"))
	if err != nil {
		return fail(deps, err, err.Error())
	}
	if err = plan.Validate(input); err != nil {
		return fail(deps, ErrState, err.Error())
	}
	// The current control plane has no opaque plan-amendment authority. Actor is
	// declarative metadata, so accepting a privileged label would be forgeable.
	return fail(deps, ErrState, "amend-plan requires structural plan amendment authority; no such authority exists")
}

func runApprovePlan(args []string, deps Dependencies) int {
	values, err := parseFlags(args, flagRules{required: []string{"--workflow-id", "--revision", "--actor"}, repeatable: []string{"--approve-exception"}})
	if err != nil {
		return fail(deps, err, err.Error())
	}
	revision, err := values.int64("--revision")
	if err != nil {
		return fail(deps, err, err.Error())
	}
	return withStore(deps, func(s *store.Store) error {
		current, err := plan.NewService(s, deps.Now).Approve(context.Background(), values.one("--workflow-id"), revision, values.one("--actor"), values.all("--approve-exception"))
		if err != nil {
			return err
		}
		return writeSuccess(deps, map[string]any{"workflow": current}, "workflow begin-implementation")
	})
}
func runReady(args []string, deps Dependencies) int {
	values, err := parseFlags(args, flagRules{required: []string{"--workflow-id"}})
	if err != nil {
		return fail(deps, err, err.Error())
	}
	return withReadStore(deps, func(s *store.Store) error {
		units, err := plan.NewService(s, deps.Now).Ready(context.Background(), values.one("--workflow-id"))
		if err != nil {
			return err
		}
		return writeSuccess(deps, map[string]any{"units": units}, "workflow claim-unit")
	})
}
func runBeginImplementation(args []string, deps Dependencies) int {
	values, revision, err := mutationFlags(args, nil)
	if err != nil {
		return fail(deps, err, err.Error())
	}
	return withStore(deps, func(s *store.Store) error {
		current, err := workflow.New(s, deps.Now).Transition(context.Background(), values.one("--workflow-id"), revision, workflow.BeginImplementation, values.one("--actor"))
		if err != nil {
			return err
		}
		return writeSuccess(deps, map[string]any{"workflow": current}, nextAction(current.State))
	})
}
func runComplete(args []string, deps Dependencies) int {
	values, revision, err := mutationFlags(args, []string{"--input-file"})
	if err != nil {
		return fail(deps, err, err.Error())
	}
	input, err := decodeInputFile[aggregateReviewInput](values.one("--input-file"))
	if err != nil {
		return fail(deps, err, err.Error())
	}
	if input.Verdict == nil || input.Summary == nil || input.Findings == nil {
		return fail(deps, ErrState, "aggregate review requires verdict, summary, and findings")
	}
	review := evidence.AggregateReview{Verdict: *input.Verdict, Summary: *input.Summary, Findings: *input.Findings, VerificationRuns: input.VerificationRuns, Checkpoint: input.Checkpoint, Actor: values.one("--actor")}
	if err = review.Validate(); err != nil {
		return fail(deps, ErrState, err.Error())
	}
	return withStore(deps, func(s *store.Store) error {
		ctx := context.Background()
		outcome, err := evidence.New(s, deps.Now).CompleteAggregate(ctx, values.one("--workflow-id"), revision, review)
		if err != nil {
			return err
		}
		current, err := workflow.New(s, deps.Now).Get(ctx, values.one("--workflow-id"))
		if err != nil {
			return err
		}
		next := outcome.NextAction
		return writeSuccess(deps, map[string]any{"workflow": current, "aggregate_review": outcome}, next)
	})
}

func runAuthorizeCorrection(args []string, deps Dependencies) int {
	values, err := parseFlags(args, flagRules{required: []string{"--workflow-id", "--revision", "--actor", "--input-file"}})
	if err != nil {
		return fail(deps, err, err.Error())
	}
	revision, err := values.int64("--revision")
	if err != nil {
		return fail(deps, err, err.Error())
	}
	input, err := decodeInputFile[correction.AuthorizationRequest](values.one("--input-file"))
	if err != nil || input.Validate() != nil {
		if err == nil {
			err = ErrUsage
		}
		return fail(deps, err, "authorize-correction requires aggregate_review_revision, reason, and user_direction_confirmed true")
	}
	return withStore(deps, func(s *store.Store) error {
		outcome, err := correction.NewAuthorizationService(s, deps.Now).Authorize(context.Background(), values.one("--workflow-id"), revision, values.one("--actor"), input)
		if err != nil {
			return err
		}
		current, err := workflow.New(s, deps.Now).Get(context.Background(), values.one("--workflow-id"))
		if err != nil {
			return err
		}
		return writeSuccess(deps, map[string]any{"workflow": current, "correction_authorization": outcome}, "workflow recover-aggregate")
	})
}
func runAbandon(args []string, deps Dependencies) int {
	values, revision, err := mutationFlags(args, []string{"--reason"})
	if err != nil {
		return fail(deps, err, err.Error())
	}
	return withStore(deps, func(s *store.Store) error {
		current, err := workflow.New(s, deps.Now).Abandon(context.Background(), values.one("--workflow-id"), revision, values.one("--actor"), values.one("--reason"))
		if err != nil {
			return err
		}
		return writeSuccess(deps, map[string]any{"workflow": current}, "none")
	})
}
func mutationFlags(args []string, extra []string) (flagValues, int64, error) {
	required := append([]string{"--workflow-id", "--revision", "--actor"}, extra...)
	values, err := parseFlags(args, flagRules{required: required})
	if err != nil {
		return nil, 0, err
	}
	revision, err := values.int64("--revision")
	return values, revision, err
}

func runClaim(command string, args []string, deps Dependencies) int {
	rules := flagRules{required: []string{"--workflow-id", "--unit-id", "--revision", "--actor", "--handle-dir"}}
	if command == "claim-unit" {
		rules.boolean = []string{"--print-claim-handle-secret-once"}
	}
	values, err := parseFlags(args, rules)
	if err != nil {
		return fail(deps, err, err.Error())
	}
	revision, err := values.int64("--revision")
	if err != nil {
		return fail(deps, err, err.Error())
	}
	return withStore(deps, func(s *store.Store) error {
		manager := handles.New(s, deps.Now, deps.Entropy)
		var result handles.IssueResult
		var err error
		if command == "recover-review" {
			result, err = manager.RecoverReviewAt(context.Background(), values.one("--workflow-id"), values.one("--unit-id"), revision, values.one("--actor"), values.one("--handle-dir"))
		} else if command == "recover-unit-claim" {
			result, err = manager.RecoverAt(context.Background(), values.one("--workflow-id"), values.one("--unit-id"), revision, values.one("--actor"), values.one("--handle-dir"))
		} else if values.one("--print-claim-handle-secret-once") != "" {
			result, err = manager.IssueDebugAt(context.Background(), values.one("--workflow-id"), values.one("--unit-id"), revision, values.one("--actor"), values.one("--handle-dir"))
		} else {
			result, err = manager.IssueAt(context.Background(), values.one("--workflow-id"), values.one("--unit-id"), revision, values.one("--actor"), values.one("--handle-dir"))
		}
		if err != nil {
			return err
		}
		data := map[string]any{}
		if result.Secret != "" {
			data["claim_secret"] = result.Secret
		} else {
			data["handle_path"] = result.Path
		}
		next := "workflow unit-tdd"
		if command == "recover-review" {
			next = "workflow unit-review"
		}
		return writeSuccess(deps, data, next)
	})
}

func runRecoverAggregate(args []string, deps Dependencies) int {
	values, err := parseFlags(args, flagRules{required: []string{"--workflow-id", "--revision", "--actor", "--handle-dir"}, optional: []string{"--unit-id", "--input-file"}})
	if err != nil {
		return fail(deps, err, err.Error())
	}
	if (values.one("--unit-id") == "") == (values.one("--input-file") == "") {
		return fail(deps, ErrUsage, "recover-aggregate requires exactly one of --unit-id or --input-file")
	}
	revision, err := values.int64("--revision")
	if err != nil {
		return fail(deps, err, err.Error())
	}
	var request handles.AggregateRecoveryRequest
	if values.one("--input-file") != "" {
		request, err = decodeInputFile[handles.AggregateRecoveryRequest](values.one("--input-file"))
		if err != nil {
			return fail(deps, err, err.Error())
		}
	}
	return withStore(deps, func(s *store.Store) error {
		manager := handles.New(s, deps.Now, deps.Entropy)
		type outputHandle struct {
			UnitID       string `json:"unit_id"`
			UnitRevision int64  `json:"unit_revision"`
			Actor        string `json:"actor"`
			HandlePath   string `json:"handle_path"`
		}
		var output []outputHandle
		if values.one("--unit-id") != "" {
			issued, issueErr := manager.RecoverAggregateAt(context.Background(), values.one("--workflow-id"), values.one("--unit-id"), revision, values.one("--actor"), values.one("--handle-dir"))
			if issueErr != nil {
				return issueErr
			}
			output = append(output, outputHandle{values.one("--unit-id"), issued.UnitRevision, values.one("--actor"), issued.Path})
		} else {
			batch, issueErr := manager.RecoverAggregateBatchAt(context.Background(), values.one("--workflow-id"), revision, values.one("--actor"), values.one("--handle-dir"), request)
			if issueErr != nil {
				return issueErr
			}
			actors, units := map[string]string{}, []string{}
			for _, assignment := range request.Assignments {
				actors[assignment.UnitID] = assignment.Actor
			}
			for _, group := range request.Groups {
				units = append(units, group.UnitIDs...)
			}
			for i, issued := range batch.Handles {
				output = append(output, outputHandle{units[i], issued.UnitRevision, actors[units[i]], issued.Path})
			}
		}
		return writeSuccess(deps, map[string]any{"handles": output}, "workflow unit-tdd")
	})
}
func unitValues(args []string, withInput bool) (flagValues, int64, error) {
	required := []string{"--workflow-id", "--unit-id", "--revision", "--actor", "--claim-handle"}
	if withInput {
		required = append(required, "--input-file")
	}
	values, err := parseFlags(args, flagRules{required: required})
	if err != nil {
		return nil, 0, err
	}
	revision, err := values.int64("--revision")
	return values, revision, err
}
func runUnitTDD(args []string, deps Dependencies) int {
	values, revision, err := unitValues(args, true)
	if err != nil {
		return fail(deps, err, err.Error())
	}
	input, err := decodeInputFile[evidence.TDDRecord](values.one("--input-file"))
	if err != nil {
		return fail(deps, err, err.Error())
	}
	if err = input.Validate(); err != nil {
		return fail(deps, ErrState, err.Error())
	}
	return withStore(deps, func(s *store.Store) error {
		manager := handles.New(s, deps.Now, deps.Entropy)
		ctx := context.Background()
		service := evidence.New(s, deps.Now)
		if _, err := manager.UseForMutation(ctx, values.one("--claim-handle"), values.one("--workflow-id"), values.one("--unit-id"), revision, values.one("--actor"), handles.TDD, func(tx *sql.Tx, _ handles.Handle) error {
			return service.RecordTDDAsTx(ctx, tx, values.one("--workflow-id"), values.one("--unit-id"), revision, values.one("--actor"), input)
		}); err != nil {
			return err
		}
		return writeSuccess(deps, map[string]any{"unit_id": values.one("--unit-id"), "unit_revision": revision, "state": "reviewing"}, "workflow handoff-review")
	})
}
func runUnitReview(args []string, deps Dependencies) int {
	values, revision, err := unitValues(args, true)
	if err != nil {
		return fail(deps, err, err.Error())
	}
	input, err := decodeInputFile[reviewInput](values.one("--input-file"))
	if err != nil {
		return fail(deps, err, err.Error())
	}
	if input.Verdict == nil || input.Summary == nil || input.Findings == nil {
		return fail(deps, ErrState, "review requires verdict, summary, and findings")
	}
	review := evidence.Review{WorkflowID: values.one("--workflow-id"), UnitID: values.one("--unit-id"), Revision: revision, Actor: values.one("--actor"), Verdict: *input.Verdict, Summary: *input.Summary, Findings: *input.Findings}
	if input.PlanImpact != nil {
		review.PlanImpact = *input.PlanImpact
	}
	if err := review.Validate(); err != nil {
		return fail(deps, ErrState, err.Error())
	}
	return withStore(deps, func(s *store.Store) error {
		manager := handles.New(s, deps.Now, deps.Entropy)
		ctx := context.Background()
		service := evidence.New(s, deps.Now)
		var outcome evidence.ReviewOutcome
		_, err := manager.UseForMutationAtPurpose(ctx, values.one("--claim-handle"), review.WorkflowID, review.UnitID, revision, review.Actor, handles.Review, handles.PurposeReview, func(tx *sql.Tx, handle handles.Handle) error {
			var mutationErr error
			outcome, mutationErr = service.RecordReviewTx(ctx, tx, review)
			if mutationErr == nil {
				_, mutationErr = tx.ExecContext(ctx, `UPDATE handles SET state='revoked' WHERE claim_id=?`, handle.ClaimID)
			}
			return mutationErr
		})
		if err != nil {
			return err
		}
		next := "workflow unit-complete"
		if review.Verdict == evidence.Corrections {
			next = "workflow recover-unit-claim"
		}
		if outcome.PlanRevisionRequired {
			next = "aion revise plan"
		}
		return writeSuccess(deps, map[string]any{"unit_id": review.UnitID, "unit_revision": outcome.NextRevision, "plan_revision_required": outcome.PlanRevisionRequired}, next)
	})
}
func runUnitComplete(args []string, deps Dependencies) int {
	values, revision, err := unitValues(args, false)
	if err != nil {
		return fail(deps, err, err.Error())
	}
	return withStore(deps, func(s *store.Store) error {
		ctx := context.Background()
		manager := handles.New(s, deps.Now, deps.Entropy)
		wf, err := workflow.New(s, deps.Now).Get(ctx, values.one("--workflow-id"))
		if err != nil {
			return err
		}
		service := evidence.New(s, deps.Now)
		if _, err = manager.UseForMutation(ctx, values.one("--claim-handle"), wf.ID, values.one("--unit-id"), revision, values.one("--actor"), handles.Complete, func(tx *sql.Tx, handle handles.Handle) error {
			return service.CompleteUnitWithClaimTx(ctx, tx, wf.ID, values.one("--unit-id"), handle.ClaimID, revision, wf.Revision, values.one("--actor"))
		}); err != nil {
			return err
		}
		wf, err = workflow.New(s, deps.Now).Get(ctx, wf.ID)
		if err != nil {
			return err
		}
		return writeSuccess(deps, map[string]any{"workflow": wf, "unit_id": values.one("--unit-id"), "unit_revision": revision, "state": "done"}, nextAction(wf.State))
	})
}

func withStore(deps Dependencies, action func(*store.Store) error) int {
	var s *store.Store
	var err error
	if deps.DataHome == "" {
		s, err = store.Open(context.Background(), deps.ProjectRoot)
	} else {
		inspection, inspectErr := project.Inspect(deps.ProjectRoot, deps.DataHome)
		if inspectErr != nil {
			err = inspectErr
		} else if len(inspection.Legacy.Candidates) != 0 && !inspection.Initialized {
			err = project.ErrMigrationRequired
		} else {
			s, err = store.OpenProject(context.Background(), inspection.Project, inspection.Paths)
			if err == nil {
				current, discoverErr := project.DiscoverLegacy(inspection.Project)
				if discoverErr != nil {
					err = discoverErr
				} else {
					err = gateLegacy(s, inspection.Project.ID, current)
				}
			}
		}
	}
	if err == nil {
		defer s.Close()
		err = action(s)
	}
	if err != nil {
		return fail(deps, err, err.Error())
	}
	return 0
}
func withReadStore(deps Dependencies, action func(*store.Store) error) int {
	return withReadStoreOrUninitialized(deps, action, nil)
}
func withReadStoreOrUninitialized(deps Dependencies, action func(*store.Store) error, uninitialized func() error) int {
	var s *store.Store
	var err error
	if deps.DataHome == "" {
		opened, openErr := store.OpenReadOnly(context.Background(), deps.ProjectRoot)
		err = openErr
		if err == nil && opened.State == store.Initialized {
			s = opened.Store
		} else if err == nil {
			err = tui.ErrUninitialized
		}
	} else {
		var inspection project.Inspection
		inspection, err = project.Inspect(deps.ProjectRoot, deps.DataHome)
		if err == nil {
			s, err = readProjectStore(inspection)
		}
	}
	if errors.Is(err, tui.ErrUninitialized) && uninitialized != nil {
		err = uninitialized()
	} else if err == nil {
		defer s.Close()
		err = action(s)
	}
	if err != nil {
		return fail(deps, err, err.Error())
	}
	return 0
}
func readProjectStore(inspection project.Inspection) (*store.Store, error) {
	opened, err := store.OpenProjectReadOnly(context.Background(), inspection.Project, inspection.Paths)
	if err != nil {
		return nil, err
	}
	if opened.State == store.Uninitialized {
		if err := project.GateLegacy(inspection.Legacy, ""); err != nil {
			return nil, err
		}
		return nil, tui.ErrUninitialized
	}
	current, err := project.DiscoverLegacy(inspection.Project)
	if err == nil {
		err = gateLegacy(opened.Store, inspection.Project.ID, current)
	}
	if err != nil {
		_ = opened.Store.Close()
		return nil, err
	}
	return opened.Store, nil
}
func gateLegacy(s *store.Store, projectID string, discovery project.LegacyDiscovery) error {
	acknowledged, table := 0, 0
	_ = s.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='consolidation_acknowledgements'`).Scan(&table)
	if table != 0 {
		_ = s.DB().QueryRow(`SELECT count(*) FROM consolidation_acknowledgements WHERE project_id=? AND candidate_set_id=?`, projectID, discovery.CandidateSetID).Scan(&acknowledged)
	}
	value := ""
	if acknowledged != 0 {
		value = discovery.CandidateSetID
	}
	return project.GateLegacy(discovery, value)
}
func writeSuccess(deps Dependencies, data any, next string) error {
	return envelope.WriteSuccess(deps.Stdout, data, nil, next)
}
func classify(err error) envelope.ExitCode {
	switch {
	case errors.Is(err, ErrUsage):
		return envelope.Usage
	case errors.Is(err, store.ErrCASMismatch):
		return envelope.CAS
	case errors.Is(err, handles.ErrInvalid), errors.Is(err, handles.ErrExpired), errors.Is(err, handles.ErrUnsafePath), errors.Is(err, handles.ErrUnsafePermissions), errors.Is(err, evidence.ErrInvalidHandle):
		return envelope.Handle
	case errors.Is(err, ErrState), errors.Is(err, correction.ErrAuthorizationForbidden), errors.Is(err, tui.ErrUninitialized), errors.Is(err, sql.ErrNoRows), errors.Is(err, project.ErrMigrationRequired), errors.Is(err, consolidate.ErrInvalidManifest), errors.Is(err, consolidate.ErrConflict), errors.Is(err, workflow.ErrInvalidName), errors.Is(err, workflow.ErrInvalidActor), errors.Is(err, workflow.ErrInvalidTransition), errors.Is(err, workflow.ErrInvalidNormativeArtifact), errors.Is(err, workflow.ErrNotFound), errors.Is(err, plan.ErrNotFound), errors.Is(err, plan.ErrInvalidApproval), errors.Is(err, evidence.ErrInvalidState), errors.Is(err, evidence.ErrReviewRequired), errors.Is(err, handles.ErrIdentityCollision), errors.Is(err, handles.ErrRecoveryForbidden), errors.Is(err, handles.ErrAlreadyClaimed), errors.Is(err, handles.ErrInvalidState), strings.Contains(strings.ToLower(err.Error()), "database is locked"), strings.Contains(err.Error(), "SQLITE_BUSY"):
		return envelope.State
	default:
		return envelope.Internal
	}
}
func fail(deps Dependencies, err error, message string) int {
	code := classify(err)
	_ = envelope.WriteFailure(deps.Stderr, code, message)
	return int(code)
}
func equalArgs(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
func nextAction(state workflow.State) string {
	return workflow.NextAction(state)
}
func writeHelp(w io.Writer, body string) {
	fmt.Fprint(w, body)
	if !strings.HasSuffix(body, "\n") {
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, helpEpilogue)
}

const rootHelp = `Usage: pitcrew <command> [options]

Commands:
  agent brief
  install codex|opencode|claude|pi
  project inspect|consolidate
  context inspect|initialize|record
  delivery start|update|show|search|active
  tui
  principles
  workflow new|continue|show|progress|request-capability|explore|spec|design|plan|amend-plan|approve-plan
  workflow list-ready-units|begin-implementation|complete|authorize-correction|abandon
  workflow claim-unit|recover-unit-claim|recover-aggregate|handoff-review|recover-review|unit-tdd|unit-review|unit-complete

Global options:
  --help
  --version
`
const workflowHelp = `Usage: pitcrew workflow <subcommand> [options]

Commands: new, continue, show, progress, request-capability, explore, spec, design, plan, amend-plan, approve-plan, list-ready-units,
  begin-implementation, complete, authorize-correction, abandon, claim-unit, recover-unit-claim, recover-aggregate, handoff-review, recover-review,
  unit-tdd, unit-review, unit-complete
`
const deliveryHelp = `Usage: pitcrew delivery <subcommand> [options]

Commands: start, update, show, search, active
`
const principlesHelp = `Usage: pitcrew principles [--json]
`
const installHelp = `Usage: pitcrew install <codex|opencode|claude|pi>

Installs or updates PitCrew agents for one runtime.

Runtimes: codex, opencode, claude, pi
`
