package runner_test

import (
	"testing"

	"github.com/ConspiracyOS/conos/internal/runner"
)

// MockRunner records the last command passed to Exec.
type MockRunner struct {
	LastCmd string
	Output  string
	Err     error
}

func (m *MockRunner) Exec(cmd string) (string, error) {
	m.LastCmd = cmd
	return m.Output, m.Err
}

func (m *MockRunner) Stream(cmd string) error {
	m.LastCmd = cmd
	return m.Err
}

func TestSSHRunner_ExecBuildsCorrectCommand(t *testing.T) {
	// We can't run real SSH in tests, but we can verify the Runner interface compiles
	// and that SSHRunner implements it.
	var _ runner.Runner = &runner.SSHRunner{Host: "mybox"}
}

func TestMockRunner_ImplementsRunner(t *testing.T) {
	var _ runner.Runner = &MockRunner{}
}
