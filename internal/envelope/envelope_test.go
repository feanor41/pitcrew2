package envelope

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteSuccessEmitsOneDocumentWithRequiredShape(t *testing.T) {
	var out bytes.Buffer
	err := WriteSuccess(&out, map[string]any{"revision": 1}, nil, "workflow explore")
	if err != nil {
		t.Fatal(err)
	}
	var got Response
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || len(got.Warnings) != 0 || got.NextAction != "workflow explore" || !strings.HasSuffix(out.String(), "\n") {
		t.Fatalf("response = %#v; raw = %q", got, out.String())
	}
}

func TestWriteFailureEmitsOneLineAndCodesAreStable(t *testing.T) {
	codes := []ExitCode{Internal, Usage, State, CAS, Handle}
	for want, code := range codes {
		if int(code) != want+1 {
			t.Fatalf("code[%d] = %d", want, code)
		}
	}
	var out bytes.Buffer
	if err := WriteFailure(&out, CAS, "revision mismatch"); err != nil {
		t.Fatal(err)
	}
	if strings.Count(strings.TrimSuffix(out.String(), "\n"), "\n") != 0 {
		t.Fatalf("failure is not one line: %q", out.String())
	}
	var got Failure
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || got.OK || got.Error.Code != "cas" || got.Error.Message != "revision mismatch" {
		t.Fatalf("failure = %#v, %v", got, err)
	}
}
