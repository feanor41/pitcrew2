package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/fmazzalomo/pitcrew/internal/envelope"
	"github.com/fmazzalomo/pitcrew/internal/roadmap"
)

func TestRoadmapCLIProvidesFiveOfflineCommandsWithTextJSONParity(t *testing.T) {
	for _, name := range []string{"GITHUB_TOKEN", "GH_TOKEN", "HTTP_PROXY", "HTTPS_PROXY"} {
		t.Setenv(name, "")
	}
	root := t.TempDir()
	captureInput := writeInput(t, root, "capture.json", `{"title":"Durable finding","body":"Publish later","provenance":{"kind":"conversation","issue":168}}`)
	captured := mustRoadmapOK(t, runAt(t, root, "roadmap", "capture", "--input-file", captureInput))
	id := regexp.MustCompile(`rm-[0-9a-f]{24}`).FindString(captured.stdout)
	if id == "" || !strings.Contains(captured.stdout, "Authority: local") || !strings.Contains(captured.stdout, "Next action: roadmap prepare-github") {
		t.Fatalf("capture text = %q", captured.stdout)
	}

	shownJSON := mustRoadmapOK(t, runAt(t, root, "roadmap", "show", "--roadmap-id", id, "--json"))
	shown := roadmapItemResponse(t, shownJSON.stdout)
	shownText := mustRoadmapOK(t, runAt(t, root, "roadmap", "show", "--roadmap-id", id))
	if shown.ID != id || shownText.stdout != renderRoadmapItem(shown)+"Next action: roadmap prepare-github\n" {
		t.Fatalf("show parity: item=%#v text=%q", shown, shownText.stdout)
	}

	listedJSON := mustRoadmapOK(t, runAt(t, root, "roadmap", "list", "--json"))
	listed := roadmapListResponse(t, listedJSON.stdout)
	listedText := mustRoadmapOK(t, runAt(t, root, "roadmap", "list"))
	if len(listed) != 1 || listed[0].ID != id || listedText.stdout != renderRoadmapList(listed)+"Next action: roadmap show\n" {
		t.Fatalf("list parity: items=%#v text=%q", listed, listedText.stdout)
	}

	prepareInput := writeInput(t, root, "prepare.json", `{"provider":"github","namespace":"feanor41/pitcrew2"}`)
	preparedJSON := mustRoadmapOK(t, runAt(t, root, "roadmap", "prepare-github", "--roadmap-id", id, "--input-file", prepareInput, "--json"))
	prepared := roadmapPublicationResponse(t, preparedJSON.stdout)
	preparedText := mustRoadmapOK(t, runAt(t, root, "roadmap", "prepare-github", "--roadmap-id", id, "--input-file", prepareInput))
	prepareNext := "create the GitHub issue outside PitCrew, then roadmap acknowledge"
	if prepared.RoadmapID != id || preparedText.stdout != renderRoadmapPublication(prepared)+"Next action: "+prepareNext+"\n" {
		t.Fatalf("prepare parity: publication=%#v text=%q", prepared, preparedText.stdout)
	}

	ackInput := writeInput(t, root, "ack.json", `{"provider":"github","namespace":"feanor41/pitcrew2","external_id":"168","url":"https://github.com/feanor41/pitcrew2/issues/168","prepared_digest":"`+prepared.Digest+`"}`)
	ackText := mustRoadmapOK(t, runAt(t, root, "roadmap", "acknowledge", "--roadmap-id", id, "--input-file", ackInput))
	boundJSON := mustRoadmapOK(t, runAt(t, root, "roadmap", "show", "--roadmap-id", id, "--json"))
	bound := roadmapItemResponse(t, boundJSON.stdout)
	if bound.Authority != roadmap.External || ackText.stdout != renderRoadmapItem(bound)+"Next action: manage the bound GitHub issue\n" {
		t.Fatalf("acknowledge parity: item=%#v text=%q", bound, ackText.stdout)
	}
	ackJSON := mustRoadmapOK(t, runAt(t, root, "roadmap", "acknowledge", "--roadmap-id", id, "--input-file", ackInput, "--json"))
	if replay := roadmapItemResponse(t, ackJSON.stdout); replay.ID != id || replay.Authority != roadmap.External {
		t.Fatalf("acknowledge JSON replay = %#v", replay)
	}
	captureJSON := mustRoadmapOK(t, runAt(t, root, "roadmap", "capture", "--input-file", captureInput, "--json"))
	if second := roadmapItemResponse(t, captureJSON.stdout); second.ID == id || second.Authority != roadmap.Local {
		t.Fatalf("capture JSON = %#v", second)
	}
}

func TestRoadmapCLIUsesStrictFilesNamespacesClosedErrorsAndExactHelp(t *testing.T) {
	root := t.TempDir()
	valid := writeInput(t, root, "valid.json", `{"title":"title","body":"body","provenance":{}}`)
	malformed := writeInput(t, root, "malformed.json", `{`)
	unknown := writeInput(t, root, "unknown.json", `{"title":"title","body":"body","provenance":{},"priority":"high"}`)
	symlink := filepath.Join(root, "input-link.json")
	if err := os.Symlink(valid, symlink); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		code envelope.ExitCode
		args []string
	}{
		{"unknown subcommand", envelope.Usage, []string{"roadmap", "publish"}},
		{"unknown flag", envelope.Usage, []string{"roadmap", "list", "--priority", "high"}},
		{"wrong namespace", envelope.Usage, []string{"roadmap", "show", "--roadmap-id", "wf-111111111111111111111111"}},
		{"missing input", envelope.Usage, []string{"roadmap", "capture"}},
		{"malformed input", envelope.Usage, []string{"roadmap", "capture", "--input-file", malformed}},
		{"unknown input", envelope.Usage, []string{"roadmap", "capture", "--input-file", unknown}},
		{"symlink input", envelope.Usage, []string{"roadmap", "capture", "--input-file", symlink}},
		{"malformed prepare", envelope.Usage, []string{"roadmap", "prepare-github", "--roadmap-id", "rm-111111111111111111111111", "--input-file", malformed}},
		{"malformed acknowledge", envelope.Usage, []string{"roadmap", "acknowledge", "--roadmap-id", "rm-111111111111111111111111", "--input-file", malformed}},
		{"unknown item", envelope.State, []string{"roadmap", "show", "--roadmap-id", "rm-111111111111111111111111"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := runAt(t, root, test.args...)
			if got.code != int(test.code) || got.stdout != "" {
				t.Fatalf("result = %#v", got)
			}
			var failure envelope.Failure
			if json.Unmarshal([]byte(got.stderr), &failure) != nil || failure.OK || failure.Error.Code == "" || strings.Count(got.stderr, "\n") != 1 {
				t.Fatalf("failure = %q", got.stderr)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(root, ".pitcrew")); !os.IsNotExist(err) {
		t.Fatalf("failed roadmap commands initialized storage: %v", err)
	}
	for _, args := range [][]string{{"--help"}, {"roadmap", "--help"}, {"roadmap", "capture", "--help"}, {"roadmap", "show", "--help"}, {"roadmap", "list", "--help"}, {"roadmap", "prepare-github", "--help"}, {"roadmap", "acknowledge", "--help"}} {
		got := runAt(t, root, args...)
		if got.code != 0 || got.stderr != "" || !strings.Contains(got.stdout, "capture|show|list|prepare-github|acknowledge") || !strings.HasSuffix(got.stdout, helpEpilogue+"\n") {
			t.Fatalf("help %v = %#v", args, got)
		}
	}
}

func mustRoadmapOK(t *testing.T, got result) result {
	t.Helper()
	if got.code != 0 || got.stderr != "" {
		t.Fatalf("result = %#v", got)
	}
	return got
}

func roadmapItemResponse(t *testing.T, document string) roadmap.Item {
	t.Helper()
	var response struct {
		Data struct {
			Item roadmap.Item `json:"roadmap_item"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(document), &response); err != nil {
		t.Fatal(err)
	}
	return response.Data.Item
}

func roadmapListResponse(t *testing.T, document string) []roadmap.Summary {
	t.Helper()
	var response struct {
		Data struct {
			Items []roadmap.Summary `json:"roadmap_items"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(document), &response); err != nil {
		t.Fatal(err)
	}
	return response.Data.Items
}

func roadmapPublicationResponse(t *testing.T, document string) roadmap.PreparedPublication {
	t.Helper()
	var response struct {
		Data struct {
			Publication roadmap.PreparedPublication `json:"publication"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(document), &response); err != nil {
		t.Fatal(err)
	}
	return response.Data.Publication
}
