package viaduct

type Example struct{}

func (e *Example) Params() *ResourceParams {
	return NewResourceParams()
}

func (e *Example) PreflightChecks(log *logger) error {
	// Test for required parameters here.
	return nil
}

func (e *Example) OperationName() string {
	// Used for logging and error messages.
	return "Wave"
}

func (e *Example) Run(log *logger) error {
	// Logic for the resource goes here
	log.Info("Hello, world!")

	return nil
}
