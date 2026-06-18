# Viaduct

A configuration management framework written in Go.

## v0.6.1

### Fixed

- The `Download` resource no longer leaks the open file descriptor after
  writing. It now closes the file before setting permissions, which avoids
  `ETXTBSY` errors when a downstream resource execs the binary immediately. On
  failure the partial file is closed and removed

## v0.6.0

### Added

- `ChainFrom` and `ChainTo` methods on the manifest for composing a chain with
  an existing resource: `ChainFrom` starts the chain after a resource, and
  `ChainTo` makes a resource run after the chain
- A `ResourceChain` return type for `Chain`, `ChainFrom` and `ChainTo`, with
  nil-safe `Last` and `First` helpers for referencing the ends of a chain

## v0.5.2

### Added

- A `Chain` helper on the manifest for wiring a linear sequence of resources
  where each one depends on the previous, in a single call
- A `CreateDirIfMissing` option on the `File`, `Template`, `Link` and `Download`
  resources to create the parent directory on demand, plus a `CreateFileP`
  shorthand for files

## v0.5.1

### Added

- A `--stdout` flag and `VIADUCT_STDOUT` env var to log non-error output to
  STDOUT instead of STDERR. Errors still go to STDERR.

## v0.5.0

### Added

- A new `User` resource for managing users and groups
- A new `Service` resource for managing systemd services
- A new `Archive` resource for extracting tar and zip archives, with strip and
  pick options for pulling single binaries out of release tarballs
- A new `Template` resource for rendering Go templates from disk
- A new `Sysctl` resource for writing and applying kernel parameters
- A new `Line` resource for editing individual lines in a file that isn't fully
  managed by `File`
- A `Lock` option on the `Execute` resource, and an `ExecLocked` helper, for
  taking the global lock
- Locked apt and dpkg helpers: `DistUpgrade`, `AptAutoremove`, `AptHold` and
  `InstallDeb`
- Docker-based integration tests that run across Ubuntu, Debian, Fedora and Arch

### Changed

- Pinned GitHub Actions to commit SHAs and restricted the workflow token
  permissions

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
