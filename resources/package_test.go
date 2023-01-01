package resources

// I don't want to have to install and uninstall packages on a system because
// it normally requires sudo. This is actually the one resource that would
// really benefit from an acceptance, given the different distributions etc.
//
// Perhaps we can use a container for each different distribution. If we're
// going down that path, then probably all acceptance tests should happen
// inside a container for each supported distribution.
