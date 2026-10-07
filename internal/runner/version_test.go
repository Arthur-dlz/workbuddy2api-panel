package runner

import (
	"os"
	"strings"
	"testing"
)

func TestAppVersionIsReleaseVersion(t *testing.T) {
	if appVersion != "1.5.0" {
		t.Fatalf("appVersion=%q, want 1.5.0", appVersion)
	}
	if strings.TrimSpace(appVersion) == "" {
		t.Fatal("appVersion must not be empty")
	}
}

func TestBinaryWorkflowExtractsRunnerVersionAndRejectsEmpty(t *testing.T) {
	workflow, err := os.ReadFile("../../.github/workflows/go-binaries.yml")
	if err != nil {
		t.Fatal(err)
	}
	s := string(workflow)
	for _, want := range []string{
		`const appVersion = "\K[^" ]+' internal/runner/runner.go`,
		`if [[ -z "$V" ]]`,
		`if [[ -z "${{ steps.ver.outputs.v }}" ]]`,
		`if [[ -z "$SRC" ]]`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("workflow missing version guard %q", want)
		}
	}
	if strings.Contains(s, "cmd/server/main.go") {
		t.Fatal("workflow still extracts version from cmd/server stub")
	}
}
