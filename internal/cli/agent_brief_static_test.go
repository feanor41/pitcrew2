package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/agentbrief"
	"github.com/fmazzalomo/pitcrew/internal/maxims"
)

func TestAgentBriefStaticSafetyDigest(t *testing.T) {
	t.Run("uninitialized repeated text and JSON are stable and read only", func(t *testing.T) {
		root, dataHome := t.TempDir(), filepath.Join(t.TempDir(), "data")
		text1 := runBriefAt(root, dataHome, "agent", "brief", "--role", "daimon")
		text2 := runBriefAt(root, dataHome, "agent", "brief", "--role", "daimon")
		json1 := runBriefAt(root, dataHome, "agent", "brief", "--role", "daimon", "--json")
		json2 := runBriefAt(root, dataHome, "agent", "brief", "--role", "daimon", "--json")
		if text1.code != 0 || json1.code != 0 || text1 != text2 || json1 != json2 {
			t.Fatalf("unstable briefs: text=%#v/%#v json=%#v/%#v", text1, text2, json1, json2)
		}
		brief, next, err := decodeBrief(json1)
		if err != nil || brief.ContractVersion != "2" || brief.Contract.Role != "daimon" || next != brief.NextAction || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(brief.ContractDigest) {
			t.Fatalf("brief=%#v err=%v output=%q", brief, err, json1.stdout)
		}
		assertNoState(t, root, dataHome)
	})

	t.Run("closed role and context matrix fails before inspection", func(t *testing.T) {
		root, dataHome := t.TempDir(), filepath.Join(t.TempDir(), "data")
		invalid := [][]string{
			{"--role", "unknown"}, {"--role", "pc2_implementer"},
			{"--role", "daimon", "--workflow-id", "wf-x"},
			{"--role", "pc2-sdd-initializer", "--unit-id", "wu-x"},
			{"--role", "aion", "--unit-id", "wu-x"},
			{"--role", "pc2-explorer"}, {"--role", "pc2-explorer", "--workflow-id", "wf-x", "--unit-id", "wu-x"},
			{"--role", "pc2-implementer", "--workflow-id", "wf-x"},
			{"--role", "pc2-reviewer"},
		}
		for _, args := range invalid {
			got := runBriefAt(root, dataHome, append([]string{"agent", "brief"}, args...)...)
			if got.code != 2 || got.stdout != "" || got.stderr == "" {
				t.Fatalf("invalid %v = %#v", args, got)
			}
		}
		assertNoState(t, root, dataHome)
	})

	t.Run("digest identifies only stable canonical role contract", func(t *testing.T) {
		root, dataHome := t.TempDir(), ""
		digest := func(args ...string) string {
			got := runBriefAt(root, dataHome, append([]string{"agent", "brief"}, append(args, "--json")...)...)
			if got.code != 0 {
				t.Fatalf("brief %v = %#v", args, got)
			}
			value, _, err := decodeBrief(got)
			if err != nil {
				t.Fatal(err)
			}
			return value.ContractDigest
		}
		aion := digest("--role", "aion")
		if created := runAt(t, root, "workflow", "new", "--name", "Initialized", "--goal", "prove stable digest", "--actor", "aion"); created.code != 0 {
			t.Fatalf("initialize project = %#v", created)
		}
		if initialized := digest("--role", "aion"); initialized != aion {
			t.Fatalf("project state changed digest: %s != %s", initialized, aion)
		}
		reviewer, _ := agentbrief.New("pc2-reviewer", "wf-one", "")
		unit, _ := agentbrief.New("pc2-reviewer", "wf-two", "wu-one")
		if unit.ContractDigest != reviewer.ContractDigest {
			t.Fatalf("dynamic review scope changed digest: %s != %s", unit.ContractDigest, reviewer.ContractDigest)
		}
		if aion == reviewer.ContractDigest {
			t.Fatal("different canonical role contracts share a digest")
		}
	})

	t.Run("allowed commands are closed to each role authority", func(t *testing.T) {
		cases := []struct {
			role     string
			context  []string
			commands []string
		}{
			{"daimon", nil, []string{}},
			{"aion", nil, []string{"delivery", "workflow"}},
			{"pc2-sdd-initializer", nil, []string{"context inspect", "context initialize", "context record"}},
			{"pc2-explorer", []string{"--workflow-id", "wf-x"}, []string{"workflow show", "workflow explore"}},
			{"pc2-specifier", []string{"--workflow-id", "wf-x"}, []string{"workflow show", "workflow spec"}},
			{"pc2-designer", []string{"--workflow-id", "wf-x"}, []string{"workflow show", "workflow design"}},
			{"pc2-task-planner", []string{"--workflow-id", "wf-x"}, []string{"workflow show", "workflow plan"}},
			{"pc2-implementer", []string{"--workflow-id", "wf-x", "--unit-id", "wu-x"}, []string{"workflow show", "workflow list-ready-units", "workflow claim-unit", "workflow unit-tdd", "workflow unit-complete"}},
			{"pc2-reviewer", []string{"--workflow-id", "wf-x"}, []string{"workflow show", "workflow unit-review", "workflow complete"}},
		}
		for _, tc := range cases {
			values, _ := parseFlags(tc.context, flagRules{optional: []string{"--workflow-id", "--unit-id"}})
			brief, err := agentbrief.New(tc.role, values.one("--workflow-id"), values.one("--unit-id"))
			if err != nil || !reflect.DeepEqual(brief.Contract.AllowedCommands, tc.commands) {
				t.Fatalf("role %s commands=%v, want %v; err=%v", tc.role, brief.Contract.AllowedCommands, tc.commands, err)
			}
		}
	})

	t.Run("canonical contract identity and text JSON semantics agree", func(t *testing.T) {
		root := t.TempDir()
		jsonResult := runBriefAt(root, "", "agent", "brief", "--role", "daimon", "--json")
		brief, next, err := decodeBrief(jsonResult)
		if err != nil {
			t.Fatal(err)
		}
		sharedJSON := strings.Index(jsonResult.stdout, `"shared_contract":`)
		roleJSON := strings.Index(jsonResult.stdout, `},"contract_version":"2","contract_digest":`)
		if sharedJSON < 0 || roleJSON <= sharedJSON {
			t.Fatalf("JSON brief does not place shared contract before role contract: %q", jsonResult.stdout)
		}
		canonical := `{"contract_version":"2","contract":{"role":"daimon","identity":"Daimon","responsibilities":["retain the active user-visible turn while Aion remains active","wait with host-native mailbox and user steering capabilities","relay each meaningful Aion-acknowledged fact exactly once","wait no longer than five minutes before one truthful quiet notice per continuous quiet interval","forward steered input to Aion as requested state, then resume waiting","finalize only on a completed, interrupted, cancelled, timed-out, failed, blocked, needs-user, user-owned-gate, or abandoned outcome","never promise a future unsolicited update after finalizing unless the host provides a push channel","disclose missing host liveness instead of simulating it","hand intent to exactly one Aion","mutate no workflow or repository state"],"allowed_handoffs":["aion"],"allowed_commands":[],"invariants":["technical English internally","truthful evidence and progress","never expose opaque handle contents","allowed_commands is potential interface only; current authority is conveyed only by dynamic next_action and allowed_actions"],"brief_requirement":"no workflow or unit context"}}`
		if want := fmt.Sprintf("%x", sha256.Sum256([]byte(canonical))); brief.ContractDigest != want {
			t.Fatalf("digest=%s want canonical digest=%s", brief.ContractDigest, want)
		}
		text := runBriefAt(root, "", "agent", "brief", "--role", "daimon").stdout
		sharedStart := strings.Index(text, "shared_maxims_begin\n")
		sharedEnd := strings.Index(text, "shared_maxims_end\n")
		roleStart := strings.Index(text, "role_contract\n")
		if sharedStart < 0 || sharedEnd <= sharedStart || roleStart <= sharedEnd {
			t.Fatalf("text brief does not place shared maxims before role contract: %q", text)
		}
		gotMaxims := text[sharedStart+len("shared_maxims_begin\n") : sharedEnd]
		if gotMaxims != maxims.Text() {
			t.Fatalf("text brief maxims drifted: got %d bytes want %d", len(gotMaxims), len(maxims.Text()))
		}
		labels := textLabels(runBriefAt(root, "", "agent", "brief", "--role", "daimon").stdout)
		for key, want := range map[string]string{"shared_contract_version": brief.SharedContract.ContractVersion, "shared_contract_digest": brief.SharedContract.ContractDigest, "contract_version": brief.ContractVersion, "contract_digest": brief.ContractDigest, "role": brief.Contract.Role, "identity": brief.Contract.Identity, "responsibilities": strings.Join(brief.Contract.Responsibilities, "; "), "allowed_handoffs": strings.Join(brief.Contract.AllowedHandoffs, ", "), "allowed_commands": strings.Join(brief.Contract.AllowedCommands, ", "), "invariants": strings.Join(brief.Contract.Invariants, "; "), "brief_requirement": brief.Contract.BriefRequirement, "next_action": next} {
			if labels[key] != want {
				t.Fatalf("text %s=%q, JSON=%q", key, labels[key], want)
			}
		}
		if brief.SharedContract.Maxims != maxims.Text() {
			t.Fatal("JSON brief omitted canonical shared maxims")
		}
		if next != "handoff to aion" {
			t.Fatalf("Daimon next_action=%q", next)
		}
		args := []string{"agent", "brief", "--role", "aion"}
		contextBrief, contextNext, err := decodeBrief(runBriefAt(root, "", append(args, "--json")...))
		contextText := textLabels(runBriefAt(root, "", args...).stdout)
		if err != nil || contextBrief.Context == nil || contextBrief.Context.Continuity == nil || contextText["context_kind"] != contextBrief.Context.Kind || contextText["next_action"] != contextNext {
			t.Fatalf("text/JSON context mismatch: brief=%#v labels=%v err=%v", contextBrief, contextText, err)
		}
	})
}

func decodeBrief(got result) (agentbrief.Brief, string, error) {
	var response struct {
		OK   bool `json:"ok"`
		Data struct {
			Brief agentbrief.Brief `json:"brief"`
		} `json:"data"`
		Warnings   []string `json:"warnings"`
		NextAction string   `json:"next_action"`
	}
	err := json.Unmarshal([]byte(got.stdout), &response)
	if err == nil && (!response.OK || response.Warnings == nil) {
		err = fmt.Errorf("invalid success envelope")
	}
	return response.Data.Brief, response.NextAction, err
}

func textLabels(text string) map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		if key, value, ok := strings.Cut(line, ": "); ok {
			result[key] = value
		}
	}
	return result
}

func runBriefAt(root, dataHome string, args ...string) result {
	var stdout, stderr bytes.Buffer
	code := Run(args, Dependencies{Stdout: &stdout, Stderr: &stderr, ProjectRoot: root, DataHome: dataHome})
	return result{code, stdout.String(), stderr.String()}
}

func assertNoState(t *testing.T, root, dataHome string) {
	t.Helper()
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("checkout mutated: entries=%v err=%v", entries, err)
	}
	if _, err := os.Stat(dataHome); !os.IsNotExist(err) {
		t.Fatalf("data home mutated: %v", err)
	}
}
