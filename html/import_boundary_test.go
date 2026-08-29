package html_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestHTMLProductionImportBoundary(t *testing.T) {
	command := exec.Command("go", "list", "-deps", "./...")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list HTML dependencies: %v\n%s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		if dependency == "net/http" || strings.Contains(dependency, "/websocket") ||
			dependency == "github.com/bnema/vev" || strings.HasPrefix(dependency, "github.com/bnema/vev/") {
			t.Errorf("forbidden HTML production dependency %q", dependency)
		}
	}

	command = exec.Command("go", "list", "-deps", "github.com/bnema/vev-vt/core")
	output, err = command.CombinedOutput()
	if err != nil {
		t.Fatalf("go list core dependencies: %v\n%s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		if dependency == "github.com/bnema/vev-vt/html" || strings.HasPrefix(dependency, "github.com/bnema/vev-vt/html/") {
			t.Errorf("core imports concrete frontend %q", dependency)
		}
	}
}
