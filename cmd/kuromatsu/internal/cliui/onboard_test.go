package cliui

import (
	"io"
	"os"
	"strings"
	"testing"
)

// N12/M01: onboard told the user to "Add your API key to config.json", which
// is not enough (the model also had to be enabled and set as default). The
// next steps point to `kuromatsu model add`, which does all of it.
func TestOnboardNextSteps_PointToModelAdd(t *testing.T) {
	for _, encrypt := range []bool{false, true} {
		if steps := buildOnboardingSteps(encrypt, "/cfg/config.json"); !strings.Contains(steps, "kuromatsu model add") {
			t.Errorf("encrypt=%v: steps do not mention `kuromatsu model add`:\n%s", encrypt, steps)
		}

		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout := os.Stdout
		os.Stdout = w
		printOnboardPlain("*", encrypt, "/cfg/config.json")
		os.Stdout = stdout
		w.Close()
		out, _ := io.ReadAll(r)
		if !strings.Contains(string(out), "kuromatsu model add") {
			t.Errorf("encrypt=%v: plain output does not mention `kuromatsu model add`:\n%s", encrypt, out)
		}
	}
}
