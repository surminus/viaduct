package resources

import "github.com/surminus/viaduct"

// Example is an example resource. Add a description of your resource
// and what it does here.
type Example struct{}

// Params are globally available parameters for interacting with Viaduct during
// a run.
func (a *Example) Params() *viaduct.ResourceParams {
	return viaduct.NewResourceParams()
}

// PreflightChecks is an important step to ensure that:
// 1. Required resource parameters have been assigned
// 2. Any optional parameters have default values if needed
// 3. Any other checks that may cause an error during a run
func (a *Example) PreflightChecks(log *viaduct.Logger) error {
	// Test for required parameters here.
	return nil
}

// OperationName is used for logging and error messages, and will be
// something like "Create" or "Delete".
func (a *Example) OperationName() string {
	// Used for logging and error messages.
	return "Wave"
}

// Run actually performs the work for the resource.
func (a *Example) Run(log *viaduct.Logger) error {
	log.Info("Hello, world!")

	return nil
}
