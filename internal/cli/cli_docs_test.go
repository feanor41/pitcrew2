package cli

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func readCLIReference(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile("../../docs/cli-reference.md")
	if err != nil {
		t.Fatalf("read CLI reference: %v", err)
	}
	return string(body)
}

func TestCLIReferenceOffersProgressiveNavigation(t *testing.T) {
	doc := readCLIReference(t)
	ordered := []string{
		"<!-- cli-docs:navigation:start -->",
		`<a id="choose-a-path"></a>`,
		"## Choose a path",
		"[Install PitCrew agents](#runtime-installation)",
		"[Inspect a project](#project-inspection-and-consolidation)",
		"[Run a direct delivery](#delivery-routing-and-admission)",
		"[Run a full workflow](#full-workflow-lifecycle)",
		"[Look up every command](#command-catalog)",
		"<!-- cli-docs:navigation:end -->",
	}
	position := 0
	for _, want := range ordered {
		next := strings.Index(doc[position:], want)
		if next < 0 {
			t.Fatalf("CLI reference is missing ordered navigation contract %q", want)
		}
		position += next + len(want)
	}
	for _, marker := range []string{
		"<!-- cli-docs:command-catalog:start -->",
		"<!-- cli-docs:command-catalog:end -->",
	} {
		if !strings.Contains(doc, marker) {
			t.Fatalf("CLI reference is missing stable catalog marker %q", marker)
		}
	}
}

func TestCLIReferenceKeepsFourBoundedDiagramsWithTextFallbacks(t *testing.T) {
	doc := readCLIReference(t)
	diagramIDs := []string{"admission-routing", "direct-delivery", "aggregate", "unit-authority"}
	for _, id := range diagramIDs {
		startMarker := "<!-- cli-docs:diagram:" + id + ":start -->"
		endMarker := "<!-- cli-docs:diagram:" + id + ":end -->"
		if strings.Count(doc, startMarker) != 1 || strings.Count(doc, endMarker) != 1 {
			t.Fatalf("CLI reference must contain unique %s diagram markers", id)
		}
		start := strings.Index(doc, startMarker)
		end := strings.Index(doc, endMarker)
		if start < 0 || end < 0 || end <= start {
			t.Fatalf("CLI reference is missing bounded %s diagram markers", id)
		}
		block := doc[start : end+len(endMarker)]
		if strings.Count(block, "```mermaid") != 1 {
			t.Fatalf("%s diagram must contain exactly one Mermaid fence", id)
		}
		if !strings.Contains(block, "**Text fallback.**") {
			t.Fatalf("%s diagram must keep an adjacent textual fallback", id)
		}
		if lines := strings.Count(block, "\n"); lines > 52 {
			t.Fatalf("%s diagram block has %d lines; want at most 52", id, lines)
		}
	}
	if got := strings.Count(doc, "```mermaid"); got != len(diagramIDs) {
		t.Fatalf("CLI reference has %d Mermaid diagrams; want exactly %d", got, len(diagramIDs))
	}
}

func TestCLIReferenceInventoryEqualsProductionWorkflowCommands(t *testing.T) {
	doc := readCLIReference(t)
	matrixPattern := regexp.MustCompile("(?m)^\\| `workflow ([a-z-]+)` \\|")
	profilePattern := regexp.MustCompile("(?m)^<!-- cli-docs:profile:workflow-([a-z-]+):start -->$")
	matrix, profiles := map[string]int{}, map[string]int{}
	for _, match := range matrixPattern.FindAllStringSubmatch(doc, -1) {
		matrix[match[1]]++
	}
	for _, match := range profilePattern.FindAllStringSubmatch(doc, -1) {
		profiles[match[1]]++
	}
	if len(matrix) != len(workflowCommands) || len(profiles) != len(workflowCommands) {
		t.Fatalf("documented workflow inventory matrix=%d profiles=%d; production=%d", len(matrix), len(profiles), len(workflowCommands))
	}
	for command := range workflowCommands {
		if matrix[command] != 1 || profiles[command] != 1 {
			t.Fatalf("workflow %s must appear exactly once in matrix and profiles", command)
		}
		anchor := `<a id="workflow-` + command + `"></a>`
		if strings.Count(doc, anchor) != 1 {
			t.Fatalf("workflow %s must have one stable profile anchor", command)
		}
		startMarker := "<!-- cli-docs:profile:workflow-" + command + ":start -->"
		endMarker := "<!-- cli-docs:profile:workflow-" + command + ":end -->"
		if strings.Count(doc, endMarker) != 1 {
			t.Fatalf("workflow %s must have one profile end marker", command)
		}
		start, end := strings.Index(doc, startMarker), strings.Index(doc, endMarker)
		profile := doc[start : end+len(endMarker)]
		if strings.Count(profile, "#### `pitcrew workflow "+command+"`") != 1 {
			t.Fatalf("workflow %s must have one profile heading", command)
		}
		for _, field := range []string{
			"**Purpose:**", "**Syntax:**", "**Caller and behavior:**", "**Preconditions:**",
			"**Inputs:**", "**Success:**", "**Failures and recovery:**", "**Example:**",
		} {
			if strings.Count(profile, field) != 1 {
				t.Fatalf("workflow %s must have exactly one %s field", command, field)
			}
		}
	}
	for _, prohibited := range []string{"--claim-token", "--emit-plain-token"} {
		if strings.Contains(doc, prohibited) {
			t.Fatalf("CLI reference contains prohibited production token %q", prohibited)
		}
	}
	absoluteHandle := regexp.MustCompile(`/handles/[0-9a-f]{16,}\\.json`)
	if absoluteHandle.MatchString(doc) {
		t.Fatal("CLI reference example exposes a concrete opaque handle path")
	}
}

func TestCLIReferenceCoversWorkflowShortcutsAmendmentsAndAbandonment(t *testing.T) {
	doc := readCLIReference(t)
	if strings.Contains(doc, "do not skip or reverse stages") {
		t.Fatal("CLI workflow prose contradicts the implemented exploring-to-designing shortcut")
	}
	for _, want := range []string{
		"exploring --> designing: design shortcut",
		"exploring --> exploring: explore amendment",
		"specifying --> specifying: spec amendment",
		"designing --> designing: design amendment",
		"Every non-terminal aggregate state can transition to `abandoned`",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("CLI workflow map is missing transition contract %q", want)
		}
	}
}

func TestCLIReferenceDiagramsCoverRecoveryAuthorityAndTerminalBranches(t *testing.T) {
	doc := readCLIReference(t)
	for _, want := range []string{
		"Identical replay returns the same delivery identity",
		"A direct capability gap",
		"never creates a separate persisted state",
		"No-change updates are rejected",
		"A stale CAS requires one identity-specific inspection",
		"Completed and abandoned predecessors remain immutable",
		"independent revision-1 draft child with pinned normative lineage",
		"ready_to_complete --> ready_to_complete: complete corrections; persist blocker only",
		"ready_to_complete --> ready_to_complete: authorize-correction; authority only",
		"ready_to_complete --> implementing: recover-aggregate; reopen units at next revisions",
		"A corrections verdict persists only the",
		"Only\n`recover-aggregate` advances reopened units to their next revisions",
		"workflow continue --from; predecessor immutable",
		"`recover-review` is identity-bound to the original reviewer",
		"The last unit atomically advances the workflow to `ready_to_complete`",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("CLI diagrams are missing recovery semantic %q", want)
		}
	}
	for _, fictitious := range []string{"capability_blocker", "aggregate_verdict", "correction_blocker", "correction_authority", "awaiting_user"} {
		if strings.Contains(doc, fictitious) {
			t.Fatalf("CLI diagram contains fictitious state %q", fictitious)
		}
	}
	if strings.Contains(doc, "A corrections verdict itself persists a new pending revision while the aggregate") {
		t.Fatal("CLI aggregate fallback incorrectly attributes unit revision changes to the verdict")
	}
	if !strings.Contains(doc, "Every profile uses the same eight fields.") || strings.Contains(doc, "Every profile uses the same four fields.") {
		t.Fatal("CLI profile introduction must describe the exact eight-field schema")
	}
}

func TestCLIReferenceDirectDiagramHasExactPersistedTransitionMatrix(t *testing.T) {
	doc := readCLIReference(t)
	startMarker := "<!-- cli-docs:diagram:direct-delivery:start -->"
	endMarker := "<!-- cli-docs:diagram:direct-delivery:end -->"
	start, end := strings.Index(doc, startMarker), strings.Index(doc, endMarker)
	if start < 0 || end <= start {
		t.Fatal("CLI reference is missing the bounded direct-delivery diagram")
	}
	block := doc[start:end]
	edgePattern := regexp.MustCompile(`(?m)^    (in_progress|blocked|interrupted) --> (in_progress|blocked|interrupted|completed|cancelled|failed):`)
	wantDestinations := map[string]bool{
		"in_progress": true, "blocked": true, "interrupted": true,
		"completed": true, "cancelled": true, "failed": true,
	}
	for _, source := range []string{"in_progress", "blocked", "interrupted"} {
		got := map[string]int{}
		for _, match := range edgePattern.FindAllStringSubmatch(block, -1) {
			if match[1] == source {
				got[match[2]]++
			}
		}
		if len(got) != len(wantDestinations) {
			t.Fatalf("direct state %s has destinations %v; want exact persisted set %v", source, got, wantDestinations)
		}
		for destination := range wantDestinations {
			if got[destination] != 1 {
				t.Fatalf("direct transition %s -> %s appears %d times; want exactly once", source, destination, got[destination])
			}
		}
	}
	for _, selfLoop := range []string{
		"in_progress --> in_progress: changed fact",
		"blocked --> blocked: changed blocked fact",
		"interrupted --> interrupted: changed interrupted fact",
	} {
		if !strings.Contains(block, selfLoop) {
			t.Fatalf("direct diagram is missing changed-fact self-loop %q", selfLoop)
		}
	}
}

func TestCLIReferenceDistinguishesPersistedUnitStateFromEffectiveAuthority(t *testing.T) {
	doc := readCLIReference(t)
	for _, want := range []string{
		"Persisted: pending",
		"Persisted: reviewing",
		"Persisted: done",
		"Effective handle/activity status is projected separately",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("CLI unit map is missing state distinction %q", want)
		}
	}
}

func TestCLIReferenceProfilesEveryNonWorkflowCommandUniformly(t *testing.T) {
	doc := readCLIReference(t)
	profileIDs := nonWorkflowProfileIDsFromRootHelp(t)
	for _, id := range profileIDs {
		startMarker := "<!-- cli-docs:profile:" + id + ":start -->"
		endMarker := "<!-- cli-docs:profile:" + id + ":end -->"
		if strings.Count(doc, startMarker) != 1 || strings.Count(doc, endMarker) != 1 {
			t.Fatalf("CLI reference must contain unique %s profile markers", id)
		}
		start, end := strings.Index(doc, startMarker), strings.Index(doc, endMarker)
		if start < 0 || end <= start {
			t.Fatalf("CLI reference is missing bounded %s command profile", id)
		}
		profile := doc[start : end+len(endMarker)]
		if strings.Count(profile, "#### ") != 1 {
			t.Fatalf("%s profile must contain one heading", id)
		}
		anchor := `<a id="profile-` + id + `"></a>`
		if strings.Count(profile, anchor) != 1 {
			t.Fatalf("%s profile must contain one stable anchor", id)
		}
		for _, field := range []string{
			"**Purpose:**", "**Syntax:**", "**Caller and behavior:**", "**Preconditions:**",
			"**Inputs:**", "**Success:**", "**Failures and recovery:**", "**Example:**",
		} {
			if strings.Count(profile, field) != 1 {
				t.Fatalf("%s profile must contain exactly one %s field", id, field)
			}
		}
	}
	actualPattern := regexp.MustCompile(`(?m)^<!-- cli-docs:profile:([a-z-]+):start -->$`)
	actual := map[string]bool{}
	for _, match := range actualPattern.FindAllStringSubmatch(doc, -1) {
		if match[1] == "workflow" || !strings.HasPrefix(match[1], "workflow-") {
			actual[match[1]] = true
		}
	}
	if len(actual) != len(profileIDs) {
		t.Fatalf("non-workflow/root profile inventory has %d entries; root help derives %d", len(actual), len(profileIDs))
	}
	if !strings.Contains(doc, "nine minimal native role bootstraps") {
		t.Fatal("CLI reference must describe the exact native installation set")
	}
	for _, stale := range []string{"eight native agents", "nine native agents plus `pitcrew/agent-contract.md`"} {
		if strings.Contains(doc, stale) {
			t.Fatalf("CLI reference retains stale installation contract %q", stale)
		}
	}
}

func TestCLIReferenceCoversCurrentAgentBriefAndBootstrapContract(t *testing.T) {
	doc := readCLIReference(t)
	normalized := strings.Join(strings.Fields(doc), " ")
	for _, want := range []string{
		"`pitcrew agent brief` is the read-only, versioned source",
		"`contract_version`",
		"`contract_digest`",
		"Daimon and `pc2-sdd-initializer` accept no context",
		"`pc2-implementer` requires workflow and unit IDs",
		"`pc2-reviewer` requires a workflow ID",
		"nine minimal native role bootstraps",
		"recognized checksum",
		"OpenCode 1.18.23 or newer",
		"`pi-subagents` version 0.25.0",
	} {
		if !strings.Contains(normalized, strings.Join(strings.Fields(want), " ")) {
			t.Fatalf("CLI reference is missing current agent/bootstrap contract %q", want)
		}
	}
	for _, stale := range []string{
		"nine native agents plus `pitcrew/agent-contract.md`",
		"installs eight native agents",
	} {
		if strings.Contains(doc, stale) {
			t.Fatalf("CLI reference contains stale bootstrap contract %q", stale)
		}
	}
}

func nonWorkflowProfileIDsFromRootHelp(t *testing.T) []string {
	t.Helper()
	commandsText, _, ok := strings.Cut(rootHelp, "Global options:")
	if !ok {
		t.Fatal("root help is missing Global options")
	}
	ids := map[string]bool{"global-help": true, "global-version": true}
	for _, line := range strings.Split(commandsText, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 1 || strings.HasSuffix(fields[0], ":") || fields[0] == "Usage:" {
			continue
		}
		root := fields[0]
		ids[root] = true
		if len(fields) > 1 && (root == "agent" || root == "project" || root == "context" || root == "delivery") {
			for _, command := range strings.Split(fields[1], "|") {
				ids[root+"-"+command] = true
			}
		}
	}
	result := make([]string, 0, len(ids))
	for id := range ids {
		result = append(result, id)
	}
	return result
}

func TestCLIReferenceProfilesAggregateWorkflowCommandsUniformly(t *testing.T) {
	doc := readCLIReference(t)
	profileIDs := []string{
		"workflow-new", "workflow-continue", "workflow-show", "workflow-progress",
		"workflow-request-capability", "workflow-explore", "workflow-spec",
		"workflow-design", "workflow-plan", "workflow-amend-plan",
		"workflow-approve-plan", "workflow-list-ready-units",
		"workflow-begin-implementation", "workflow-complete",
		"workflow-authorize-correction", "workflow-abandon",
	}
	for _, id := range profileIDs {
		startMarker := "<!-- cli-docs:profile:" + id + ":start -->"
		endMarker := "<!-- cli-docs:profile:" + id + ":end -->"
		start, end := strings.Index(doc, startMarker), strings.Index(doc, endMarker)
		if start < 0 || end <= start {
			t.Fatalf("CLI reference is missing bounded %s command profile", id)
		}
		profile := doc[start : end+len(endMarker)]
		for _, field := range []string{
			"**Purpose:**", "**Syntax:**", "**Caller and behavior:**", "**Preconditions:**",
			"**Inputs:**", "**Success:**", "**Failures and recovery:**", "**Example:**",
		} {
			if strings.Count(profile, field) != 1 {
				t.Fatalf("%s profile must contain exactly one %s field", id, field)
			}
		}
	}
	for _, want := range []string{
		"Only a `completed` or `abandoned` predecessor can continue",
		"No success path exists in this control-plane revision",
		"corrections preserve `ready_to_complete`",
		"Inspect once after exit `3` or `4`",
	} {
		if !strings.Contains(doc, want) {
			t.Fatalf("aggregate workflow profiles are missing semantic contract %q", want)
		}
	}
}

func TestCLIReferenceDescribesAmendPlanWithoutAContradictorySuccessPath(t *testing.T) {
	doc := readCLIReference(t)
	if strings.Contains(doc, "has No success\npath exists") {
		t.Fatal("CLI lifecycle prose contains a malformed contradictory amend-plan sentence")
	}
	if strings.Count(strings.ToLower(doc), "no success\npath exists in this control-plane revision") != 1 ||
		strings.Count(doc, "No success path exists in this control-plane revision") != 1 {
		t.Fatal("CLI reference must state the unsupported amend-plan result in both the lifecycle and its profile")
	}
}

func TestCLIReferenceProfilesExposeTheExactProductionFlagMatrix(t *testing.T) {
	doc := readCLIReference(t)
	want := map[string]string{
		"workflow-new":                  "pitcrew workflow new --name <name> --goal <goal> --actor <actor>",
		"workflow-continue":             "pitcrew workflow continue --from wf-<24hex> --actor <actor>",
		"workflow-show":                 "pitcrew workflow show --workflow-id wf-<24hex> [--view <coordination|phase|unit|aggregate|audit>] [--unit-id wu-<24hex>]",
		"workflow-progress":             "pitcrew workflow progress --workflow-id wf-<24hex> --revision <n> --actor <actor> --input-file <path>",
		"workflow-request-capability":   "pitcrew workflow request-capability --workflow-id wf-<24hex> --revision <n> --actor <actor> --input-file <path>",
		"workflow-explore":              "pitcrew workflow explore --workflow-id wf-<24hex> --revision <n> --actor <actor> --input-file <path>",
		"workflow-spec":                 "pitcrew workflow spec --workflow-id wf-<24hex> --revision <n> --actor <actor> --input-file <path>",
		"workflow-design":               "pitcrew workflow design --workflow-id wf-<24hex> --revision <n> --actor <actor> --input-file <path>",
		"workflow-plan":                 "pitcrew workflow plan --workflow-id wf-<24hex> --revision <n> --actor <actor> --input-file <path>",
		"workflow-amend-plan":           "pitcrew workflow amend-plan --workflow-id wf-<24hex> --revision <n> --actor <actor> --input-file <path>",
		"workflow-approve-plan":         "pitcrew workflow approve-plan --workflow-id wf-<24hex> --revision <n> --actor <actor> [--approve-exception <unit-id> ...]",
		"workflow-list-ready-units":     "pitcrew workflow list-ready-units --workflow-id wf-<24hex>",
		"workflow-begin-implementation": "pitcrew workflow begin-implementation --workflow-id wf-<24hex> --revision <n> --actor <actor>",
		"workflow-complete":             "pitcrew workflow complete --workflow-id wf-<24hex> --revision <n> --actor <reviewer> --input-file <path>",
		"workflow-authorize-correction": "pitcrew workflow authorize-correction --workflow-id wf-<24hex> --revision <n> --actor <actor> --input-file <path>",
		"workflow-abandon":              "pitcrew workflow abandon --workflow-id wf-<24hex> --revision <n> --actor <actor> --reason <text>",
		"workflow-claim-unit":           "pitcrew workflow claim-unit --workflow-id wf-<24hex> --unit-id wu-<24hex> --revision <n> --actor <actor> --handle-dir <dir> [--print-claim-handle-secret-once]",
		"workflow-recover-unit-claim":   "pitcrew workflow recover-unit-claim --workflow-id wf-<24hex> --unit-id wu-<24hex> --revision <n> --actor <actor> --handle-dir <dir>",
		"workflow-recover-aggregate":    "pitcrew workflow recover-aggregate --workflow-id wf-<24hex> --revision <n> --actor <actor> --handle-dir <dir> (--unit-id <id>|--input-file <path>)",
		"workflow-handoff-review":       "pitcrew workflow handoff-review --workflow-id wf-<24hex> --unit-id wu-<24hex> --revision <n> --actor <reviewer> --handle-dir <dir>",
		"workflow-recover-review":       "pitcrew workflow recover-review --workflow-id wf-<24hex> --unit-id wu-<24hex> --revision <n> --actor <reviewer> --handle-dir <dir>",
		"workflow-unit-tdd":             "pitcrew workflow unit-tdd --workflow-id wf-<24hex> --unit-id wu-<24hex> --revision <n> --actor <actor> --claim-handle <path> --input-file <path>",
		"workflow-unit-review":          "pitcrew workflow unit-review --workflow-id wf-<24hex> --unit-id wu-<24hex> --revision <n> --actor <reviewer> --claim-handle <path> --input-file <path>",
		"workflow-unit-complete":        "pitcrew workflow unit-complete --workflow-id wf-<24hex> --unit-id wu-<24hex> --revision <n> --actor <actor> --claim-handle <path>",
	}
	for id, syntax := range want {
		marker := "<!-- cli-docs:profile:" + id + ":start -->"
		start := strings.Index(doc, marker)
		end := strings.Index(doc[start:], "<!-- cli-docs:profile:"+id+":end -->")
		if start < 0 || end < 0 || !strings.Contains(doc[start:start+end], "**Syntax:** `"+syntax+"`") {
			t.Fatalf("%s profile does not expose exact syntax %q", id, syntax)
		}
	}
}

func TestCLIReferenceProfilesExposeExactNonWorkflowSyntax(t *testing.T) {
	doc := readCLIReference(t)
	want := map[string]string{
		"agent":               "pitcrew agent brief --role <role> [--workflow-id wf-<24hex>] [--unit-id wu-<24hex>] [--json]",
		"agent-brief":         "pitcrew agent brief --role <role> [--workflow-id wf-<24hex>] [--unit-id wu-<24hex>] [--json]",
		"global-help":         "pitcrew --help",
		"global-version":      "pitcrew --version",
		"install":             "pitcrew install <codex|opencode|claude|pi>",
		"project":             "pitcrew project <inspect|consolidate> [options]",
		"project-inspect":     "pitcrew project inspect",
		"project-consolidate": "pitcrew project consolidate --input-file <path>",
		"context":             "pitcrew context <inspect|initialize|record> [options]",
		"context-inspect":     "pitcrew context inspect",
		"context-initialize":  "pitcrew context initialize",
		"context-record":      "pitcrew context record --actor <actor> --input-file <path>",
		"delivery":            "pitcrew delivery <start|update|show|search|active> [options]",
		"delivery-start":      "pitcrew delivery start --actor <actor> --input-file <path>",
		"delivery-update":     "pitcrew delivery update --delivery-id dl-<24hex> --revision <n> --actor <actor> --input-file <path>",
		"delivery-show":       "pitcrew delivery show --delivery-id <dl-or-wf-id>",
		"delivery-search":     "pitcrew delivery search --query <text>",
		"delivery-active":     "pitcrew delivery active",
		"tui":                 "pitcrew tui",
		"principles":          "pitcrew principles [--json]",
		"workflow":            "pitcrew workflow <subcommand> [options]",
	}
	for id, syntax := range want {
		startMarker, endMarker := "<!-- cli-docs:profile:"+id+":start -->", "<!-- cli-docs:profile:"+id+":end -->"
		start, end := strings.Index(doc, startMarker), strings.Index(doc, endMarker)
		if start < 0 || end <= start || !strings.Contains(doc[start:end], "**Syntax:** `"+syntax+"`") {
			t.Fatalf("%s profile does not expose exact syntax %q", id, syntax)
		}
	}
}

func TestCLIReferenceCoversMaterialPayloadParserStorageAndOutputInvariants(t *testing.T) {
	doc := readCLIReference(t)
	normalized := strings.Join(strings.Fields(doc), " ")
	for _, want := range []string{
		"`explore`, `spec`, and `design` accept either",
		"`schema_version` and `entries` must be supplied together",
		"`requirement`, `scenario`, or `section`",
		"`add`, `replace`, or `remove`",
		"scenario `add` requires `parent_id`",
		"exactly the six categories `stack`, `runtime`, `deployment`, `architecture`, `documentation`, and `sdd`",
		"`summary`, `scope`, `work_units`, and `max_parallel_units` are required",
		"`red_command`, `red_outcome`, `green_command`, `green_outcome`, `refactor_summary`, `validation_command`, `validation_outcome`, and `changed_paths`",
		"corrections require nonblank `findings` and `plan_impact`",
		"`user_direction_confirmed` must be `true`",
		"exactly one assignment for every selected unit",
		"Only `--approve-exception` is repeatable",
		"Positive decimal revisions are compare-and-swap expectations",
		"Mutations open central project storage only after flag, transport, and payload validation",
		"Normal claim, recovery, and review handoff success returns `data.handle_path`",
		"`--print-claim-handle-secret-once` returns `data.claim_secret` instead",
		"Every current handle lease is capped at 15 minutes",
		"`unit-tdd` returns `state: reviewing`",
		"`unit-complete` returns `state: done`",
	} {
		if !strings.Contains(normalized, strings.Join(strings.Fields(want), " ")) {
			t.Fatalf("CLI reference is missing material contract %q", want)
		}
	}
}
