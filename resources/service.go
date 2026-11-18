package resources

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/surminus/viaduct"
)

// Service manages a systemd service, allowing it to be enabled or disabled
// on boot, and started, stopped or restarted.
type Service struct {
	// Name is the name of the service unit
	Name string

	// Action is performed against the service: one of start, stop or
	// restart. Optional.
	Action string

	// Enable enables the service on boot. Optional.
	Enable bool

	// Disable disables the service on boot. Optional.
	Disable bool

	// DaemonReload runs "systemctl daemon-reload" first when systemd reports
	// that the unit's files, including its drop-ins, have changed on disk
	// since they were loaded. Optional.
	DaemonReload bool
}

// StartService is a shortcut for starting a service
func StartService(name string) *Service {
	return &Service{Name: name, Action: "start"}
}

// StopService is a shortcut for stopping a service
func StopService(name string) *Service {
	return &Service{Name: name, Action: "stop"}
}

// RestartService is a shortcut for restarting a service
func RestartService(name string) *Service {
	return &Service{Name: name, Action: "restart"}
}

// EnableService is a shortcut for enabling a service on boot
func EnableService(name string) *Service {
	return &Service{Name: name, Enable: true}
}

// DisableService is a shortcut for disabling a service on boot
func DisableService(name string) *Service {
	return &Service{Name: name, Disable: true}
}

func (s *Service) Description() string {
	return s.Name
}

func (s *Service) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

// PreflightChecks sets default values for the parameters for a particular
// resource
func (s *Service) PreflightChecks(ctx context.Context, log *viaduct.Logger) error {
	// Set required values here, and error if they are not set
	if s.Name == "" {
		return fmt.Errorf("required parameter: Name")
	}

	switch s.Action {
	case "", "start", "stop", "restart":
	default:
		return fmt.Errorf("action must be one of start, stop or restart: %s", s.Action)
	}

	if s.Enable && s.Disable {
		return fmt.Errorf("cannot set both Enable and Disable")
	}

	if s.Action == "" && !s.Enable && !s.Disable && !s.DaemonReload {
		return fmt.Errorf("requires one of Action, Enable, Disable or DaemonReload")
	}

	if err := requireRoot(ctx, "service"); err != nil {
		return err
	}

	return nil
}

func (s *Service) OperationName() string {
	var ops []string

	if s.DaemonReload {
		ops = append(ops, "DaemonReload")
	}

	if s.Enable {
		ops = append(ops, "Enable")
	}

	if s.Disable {
		ops = append(ops, "Disable")
	}

	if s.Action != "" {
		ops = append(ops, strings.ToUpper(s.Action[:1])+s.Action[1:])
	}

	return strings.Join(ops, "+")
}

func (s *Service) Run(ctx context.Context, log *viaduct.Logger) error {
	if s.DaemonReload {
		if err := s.daemonReload(ctx, log); err != nil {
			return err
		}
	}

	if s.Enable {
		if err := s.setEnabled(ctx, log, true); err != nil {
			return err
		}
	}

	if s.Disable {
		if err := s.setEnabled(ctx, log, false); err != nil {
			return err
		}
	}

	if s.Action != "" {
		return s.runAction(ctx, log)
	}

	return nil
}

func (s *Service) setEnabled(ctx context.Context, log *viaduct.Logger, enable bool) error {
	verb := "disable"
	if enable {
		verb = "enable"
	}

	state := s.enableState(ctx, log)

	// A masked unit cannot be enabled, and systemctl errors with an
	// opaque message, so fail with a clear one instead.
	if enable && state == "masked" {
		return fmt.Errorf("cannot enable masked service %s, unmask it first", s.Name)
	}

	// Static and indirect units have no [Install] section and cannot be
	// enabled or disabled, so treat them as already in the desired state
	// rather than running systemctl every time.
	if state == "static" || state == "indirect" {
		log.Noop(verb+"d", "service", s.Name, "state", state)
		return nil
	}

	enabled := state == "enabled" || state == "enabled-runtime" || state == "alias"
	if enabled == enable {
		log.Noop(verb+"d", "service", s.Name)
		return nil
	}

	if !change(ctx, log, verb+"d", "service", s.Name) {
		return nil
	}

	if err := runCommand(ctx, log, "systemctl", verb, s.Name); err != nil {
		return fmt.Errorf("systemctl %s failed for %s: %w", verb, s.Name, err)
	}

	return nil
}

func (s *Service) runAction(ctx context.Context, log *viaduct.Logger) error {
	msg := map[string]string{
		"start":   "started",
		"stop":    "stopped",
		"restart": "restarted",
	}[s.Action]

	// Starting an active service or stopping an inactive one is a noop;
	// restart always runs
	switch s.Action {
	case "start":
		if s.isActive(ctx, log) {
			log.Noop(msg, "service", s.Name)
			return nil
		}
	case "stop":
		if !s.isActive(ctx, log) {
			log.Noop(msg, "service", s.Name)
			return nil
		}
	}

	if !change(ctx, log, msg, "service", s.Name) {
		return nil
	}

	if err := runCommand(ctx, log, "systemctl", s.Action, s.Name); err != nil {
		return fmt.Errorf("systemctl %s failed for %s: %w", s.Action, s.Name, err)
	}

	return nil
}

// daemonReload reloads systemd's configuration when the unit's files have
// changed since systemd loaded them, so the steps that follow use them.
func (s *Service) daemonReload(ctx context.Context, log *viaduct.Logger) error {
	out, err := viaduct.Command{
		Name: "systemctl",
		Args: []string{"show", "--property=NeedDaemonReload", "--value", s.Name},
	}.Output(ctx, log)
	if err != nil {
		return fmt.Errorf("systemctl show failed for %s: %w", s.Name, err)
	}

	if strings.TrimSpace(string(out)) != "yes" {
		log.Noop("daemon-reload-unneeded", "service", s.Name)
		return nil
	}

	if !change(ctx, log, "daemon-reload", "service", s.Name) {
		return nil
	}

	if err := runCommand(ctx, log, "systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload failed: %w", err)
	}

	return nil
}

// enableState returns the systemctl enablement state of the unit, such as
// "enabled", "disabled", "static" or "masked". Returns an empty string if
// the state cannot be determined. It runs in a dry run too.
func (s *Service) enableState(ctx context.Context, log *viaduct.Logger) string {
	// is-enabled prints the state to stdout and exits non-zero for some
	// states (disabled, masked), so the exit code is ignored. Its stderr
	// adds nothing to the state, so it is dropped.
	out, _ := viaduct.Command{
		Name:   "systemctl",
		Args:   []string{"is-enabled", s.Name},
		Stderr: io.Discard,
	}.Output(ctx, log)

	return strings.TrimSpace(string(out))
}

// isActive reports whether the unit is running. It runs in a dry run too.
func (s *Service) isActive(ctx context.Context, log *viaduct.Logger) bool {
	c := viaduct.Command{
		Name:   "systemctl",
		Args:   []string{"is-active", "--quiet", s.Name},
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	return c.Run(ctx, log) == nil
}
