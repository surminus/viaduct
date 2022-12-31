package viaduct

type Example struct{}

func (e *Example) opts() *ResourceOptions {
	return NewResourceOptions()
}

func (e *Example) satisfy(log *logger) error {
	// Test for required parameters here.
	return nil
}

func (e *Example) operationName() string {
	// Used for logging and error messages.
	return "Wave"
}

func (e *Example) run(log *logger) error {
	// Logic for the resource goes here
	log.Info("Hello, world!")

	return nil
}
