package resources

import "github.com/surminus/viaduct"

type Example struct{}

func (e *Example) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

func (e *Example) PreflightChecks(log *viaduct.Logger) error {
	// Test for required parameters here.
	return nil
}

func (e *Example) OperationName() string {
	// Used for logging and error messages.
	return "Wave"
}

func (e *Example) Run(log *viaduct.Logger) error {
	// Logic for the resource goes here
	log.Info("Hello, world!")

	return nil
}
