package resources

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Managing services requires root and systemd, so we only test validation
// here. See package_test.go for thoughts on acceptance testing.

func TestServicePreflightChecks(t *testing.T) {
	t.Run("requires name", func(t *testing.T) {
		s := &Service{}

		err := s.PreflightChecks(testLogger)
		assert.EqualError(t, err, "required parameter: Name")
	})

	t.Run("invalid action", func(t *testing.T) {
		s := &Service{Name: "docker", Action: "reload"}

		err := s.PreflightChecks(testLogger)
		assert.EqualError(t, err, "action must be one of start, stop or restart: reload")
	})

	t.Run("enable and disable conflict", func(t *testing.T) {
		s := &Service{Name: "docker", Enable: true, Disable: true}

		err := s.PreflightChecks(testLogger)
		assert.EqualError(t, err, "cannot set both Enable and Disable")
	})

	t.Run("requires something to do", func(t *testing.T) {
		s := &Service{Name: "docker"}

		err := s.PreflightChecks(testLogger)
		assert.EqualError(t, err, "requires one of Action, Enable or Disable")
	})
}

func TestServiceHelpers(t *testing.T) {
	assert.Equal(t, &Service{Name: "docker", Action: "start"}, StartService("docker"))
	assert.Equal(t, &Service{Name: "docker", Action: "stop"}, StopService("docker"))
	assert.Equal(t, &Service{Name: "docker", Action: "restart"}, RestartService("docker"))
	assert.Equal(t, &Service{Name: "docker", Enable: true}, EnableService("docker"))
	assert.Equal(t, &Service{Name: "docker", Disable: true}, DisableService("docker"))
}

func TestServiceOperationName(t *testing.T) {
	assert.Equal(t, "Restart", (&Service{Name: "docker", Action: "restart"}).OperationName())
	assert.Equal(t, "Enable", (&Service{Name: "docker", Enable: true}).OperationName())
	assert.Equal(t, "Disable+Stop", (&Service{Name: "docker", Disable: true, Action: "stop"}).OperationName())
}
