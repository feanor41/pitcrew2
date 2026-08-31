package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestAgentBriefStaticSafetyDigest(t *testing.T) {
	t.Run("uninitialized repeated text and JSON are stable and read only", func(t *testing.T) {
		root, dataHome := t.TempDir(), filepath.Join(t.TempDir(), "data")
		before := treeSnapshot(t, root, dataHome)
		text1 := runBriefAt(root, dataHome, "agent", "brief", "--role", "daimon")
		text2 := runBriefAt(root, dataHome, "agent", "brief", "--role", "daimon")
		json1 := runBriefAt(root, dataHome, "agent", "brief", "--role", "daimon", "--json")
		json2 := runBriefAt(root, dataHome, "agent", "brief", "--role", "daimon", "--json")
		if text1.code != 0 || json1.code != 0 || text1 != text2 || json1 != json2 {
			t.Fatalf("unstable briefs: text=%#v/%#v json=%#v/%#v", text1, text2, json1, json2)
		}
		var brief struct {
			ContractVersion string `json:"contract_version"`
			ContractDigest  string `json:"contract_digest"`
			Role            string `json:"role"`
		}
		if err := json.Unmarshal([]byte(json1.stdout), &brief); err != nil || brief.ContractVersion != "agent-brief/v1" || brief.Role != "daimon" || !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(brief.ContractDigest) {
			t.Fatalf("brief=%#v err=%v output=%q", brief, err, json1.stdout)
		}
		if after := treeSnapshot(t, root, dataHome); before != after {
			t.Fatalf("brief mutated filesystem\nbefore: %s\nafter: %s", before, after)
		}
	})

	t.Run("closed role and context matrix fails before inspection", func(t *testing.T) {
		root, dataHome := t.TempDir(), filepath.Join(t.TempDir(), "data")
		before := treeSnapshot(t, root, dataHome)
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
		valid := [][]string{
			{"--role", "daimon"}, {"--role", "pc2-sdd-initializer"}, {"--role", "aion"}, {"--role", "aion", "--workflow-id", "wf-x"},
			{"--role", "pc2-explorer", "--workflow-id", "wf-x"}, {"--role", "pc2-specifier", "--workflow-id", "wf-x"},
			{"--role", "pc2-designer", "--workflow-id", "wf-x"}, {"--role", "pc2-task-planner", "--workflow-id", "wf-x"},
			{"--role", "pc2-implementer", "--workflow-id", "wf-x", "--unit-id", "wu-x"},
			{"--role", "pc2-reviewer", "--workflow-id", "wf-x"}, {"--role", "pc2-reviewer", "--workflow-id", "wf-x", "--unit-id", "wu-x"},
		}
		for _, args := range valid {
			if got := runBriefAt(root, dataHome, append([]string{"agent", "brief"}, args...)...); got.code != 0 || got.stderr != "" {
				t.Fatalf("valid %v = %#v", args, got)
			}
		}
		if after := treeSnapshot(t, root, dataHome); before != after {
			t.Fatalf("invalid briefs inspected or mutated project\nbefore: %s\nafter: %s", before, after)
		}
	})

	t.Run("digest identifies only stable canonical role contract", func(t *testing.T) {
		root, dataHome := t.TempDir(), ""
		digest := func(args ...string) string {
			got := runBriefAt(root, dataHome, append([]string{"agent", "brief"}, append(args, "--json")...)...)
			if got.code != 0 {
				t.Fatalf("brief %v = %#v", args, got)
			}
			var value struct {
				ContractDigest string `json:"contract_digest"`
			}
			if err := json.Unmarshal([]byte(got.stdout), &value); err != nil {
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
		if other := digest("--role", "aion", "--workflow-id", "wf-one"); other != aion {
			t.Fatalf("dynamic workflow changed digest: %s != %s", other, aion)
		}
		reviewer := digest("--role", "pc2-reviewer", "--workflow-id", "wf-one")
		if unit := digest("--role", "pc2-reviewer", "--workflow-id", "wf-two", "--unit-id", "wu-one"); unit != reviewer {
			t.Fatalf("dynamic review scope changed digest: %s != %s", unit, reviewer)
		}
		if aion == reviewer {
			t.Fatal("different canonical role contracts share a digest")
		}
	})
}

func runBriefAt(root, dataHome string, args ...string) result {
	var stdout, stderr bytes.Buffer
	code := Run(args, Dependencies{Stdout: &stdout, Stderr: &stderr, ProjectRoot: root, DataHome: dataHome})
	return result{code, stdout.String(), stderr.String()}
}

func treeSnapshot(t *testing.T, roots ...string) string {
	t.Helper()
	var out bytes.Buffer
	for _, root := range roots {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				t.Fatal(err)
			}
			out.WriteString(root + ":" + path + ":" + info.Mode().String() + "\n")
			return nil
		})
	}
	return out.String()
}
