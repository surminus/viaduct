# Viaduct

A configuration management framework written in Go.

## v0.3.1

### Added

- Further acceptance tests for resources
- --silent and --quiet CLI flags
- Better error handling
- A new Download resource type
- A bunch of new helper commands for use within viaduct configuration
- A viaduct.Log() function to allow users to add their own logging

### Changed

- Ensure that preflight checks run before the run actually starts
- TmpDir is now cleaned after a successful run, but left when there are errors

### Fixed

- Bug with remove packages with manjaro

## v0.3.0

See README for full examples.

### Breaking

- Scripting syntax has changed in this version; It's still possible to manually
run scripting syntax, but it's not recommended
- The standard resources are now a separate package

### Updated

- Huge number of changes to syntax
- Removed all the operational hardcoding
- Syntax shorter and simpler
- Allows custom resources implementing the ResourceAttributes interface
