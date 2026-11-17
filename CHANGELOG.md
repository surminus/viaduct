# Viaduct

A configuration management framework written in Go.

## v1.0.0 (unreleased)

### Upgrading from v0.8.0

A manifest that only uses `New`, `Add`, `Chain` and `Run` with the standard
resources needs no change. Beyond that:

- `viaduct.Attribute.X` becomes `viaduct.Attributes().X`
- `Pending`, `Success`, `Failed` and `DependencyFailed` become
  `StatusPending`, `StatusSuccess`, `StatusFailed` and
  `StatusDependencyFailed`, and a lock key is a `viaduct.LockKey`

### Added

- `Must`, for using a function that returns an error inline when the error
  would be a bug, such as `viaduct.Must(resources.NewTemplate(...))`

### Changed

- **Helpers return errors.** `FileSize`, `MatchChmod`, `MatchChown`,
  `SystemAttributes.SetUser`, `SystemAttributes.JSON`,
  `resources.EmbeddedFile` and `resources.NewTemplate` return an error rather
  than exiting.
  `Logger.Fatal` is gone. `SetUser` no longer logs
- A directory whose contents change while its ownership is applied
  recursively no longer fails when a file disappears part way through
- `ExpandPath` only expands `~` and `~/...`, and leaves every other path as it
  is
- **Lock keys have their own type, `LockKey`.** `PackageLock` and
  `PasswdLock` are `LockKey` constants, and `WithLockKey`,
  `NewResourceParamsWithLockKey` and the `LockKey` fields take one. A
  custom key is written as `viaduct.LockKey("name")`
- **Status constants have a `Status` prefix.** `Pending`, `Success`, `Failed`
  and `DependencyFailed` are `StatusPending`, `StatusSuccess`, `StatusFailed`
  and `StatusDependencyFailed`. Their values, as they appear in `--json`, are
  unchanged

- **Importing viaduct no longer does anything.** Flags are parsed, system
  attributes are read and the temporary directory is created when they are
  first needed, rather than in `init()`. A program that imports viaduct can
  define flags of its own, and nothing is left behind by a program that never
  runs a manifest
- **`Attribute` is now `Attributes()`.** `viaduct.Attribute.User` becomes
  `viaduct.Attributes().User`. The attributes are loaded on first use and
  shared, so `SetUser` and `AddCustom` are still seen everywhere.
  `LoadAttributes` returns them along with any error from reading them
- `ParseFlags` reads the command line into `Cli`, keeping anything set on it
  beforehand that the command line does not override. `Run` calls it. An
  unknown flag, or a `VIADUCT_RESOURCE_TIMEOUT` that is not a duration, is
  reported and exits with status 2. `--help` lists `VIADUCT_RESOURCE_TIMEOUT`
  and `VIADUCT_STDOUT`
- **The temporary directory is under `$TMPDIR` and is always removed.** It is
  created on first use by `TmpDir` or `TmpFile`, which now returns an error,
  and removed when `Run` finishes, whether or not the run failed. It used to be
  under `~/.viaduct/tmp` and was left behind by every failed run.
  `SystemAttributes.TmpDir` is gone

### Removed

- `ExpandPathRoot`. Use `filepath.Abs`
- `FileContents`. Use `os.ReadFile`
- `IsDirectory`. Use `DirExists`

## v0.8.0

### Added

- A new `Shell` resource for running multi-line scripts, with `Bash` and `Sh`
  shortcuts. The script runs in strict mode by default, stopping at the first
  failing command or unset variable, and under bash failing a pipeline when any
  part of it fails. `NoStrict` turns this off. Indentation shared by every line
  is removed first, so a script can be written as an indented raw string and
  heredocs inside it still work

### Changed

- **`Execute` runs its `Unless` guard in `WorkingDirectory`.** It used to run in
  viaduct's own working directory, so a guard with a relative path, such as
  `test -f build/out`, checked the wrong place and the command ran on every
  converge. A guard that relied on the old behaviour needs an absolute path
- The error from a failed `Execute` includes the underlying cause, such as
  `exit status 1` or a missing working directory
- Dependencies are updated, including `go-git/go-git/v5` to v5.19.3 and
  `golang.org/x/crypto` to v0.57.0

### Fixed

- A run could crash with a fatal concurrent map iteration and write when a
  resource finished quickly, such as an up-to-date directory, while the
  manifest was still starting the others

## v0.7.1

### Added

- A `NoRecursive` option on `Directory`, with a `DirShallow` shortcut, to apply
  the ownership to the directory itself rather than to everything inside it.
  The default is unchanged, but recursing is wrong when the directory is only a
  container for paths owned by something else, and fails outright when one of
  those paths cannot be chowned, such as a read-only mount

## v0.7.0

### Added

- A new `Group` resource for managing a group in its own right, such as one with
  a fixed GID that has to exist before the users that belong to it, with a
  `SystemGroup` shortcut
- A `PermissionsOnly` option on `File`, with a `SetPermissions` shortcut, for
  managing the mode and ownership of a file whose content belongs to something
  else. Only what is set is applied, so a mode on its own leaves ownership alone
- A `Purge` option on `Package`, with `PurgePkg` and `PurgePkgs`, to remove a
  package's configuration along with the package. apt and pacman have an
  equivalent, dnf does not, so a purge on Fedora fails in preflight rather than
  quietly doing a plain remove
- `Hold` and `Unhold` options on `Package`, with `HoldPkg`, `HoldPkgs`,
  `UnholdPkg` and `UnholdPkgs`, for holding packages back from upgrades. They
  read `apt-mark showhold` first, so an already held package is a noop. apt-mark
  only for now
- An `Args` option on `Execute`, with `ExecArgs` and `ExecArgsLocked`, to run a
  command without a shell so arguments containing spaces or shell metacharacters
  need no quoting
- Lock keys, so resources only wait for other resources in the same domain:
  `PackageLock`, `PasswdLock`, `NewResourceParamsWithLockKey` and
  `Manifest.WithLockKey`. A lock without a key still excludes everything
- `SetResourceTimeout` and `WithTimeout` on the manifest, and a
  `--resource-timeout` flag (or `VIADUCT_RESOURCE_TIMEOUT`), for how long a
  resource is given to run
- The `Download` resource verifies a SHA256 digest when `Checksum` is set

### Changed

- `dependencyTimeout` is now a timeout on how long a single resource runs, not
  on waiting. It used to be measured from the start of the run, which made it a
  budget for the whole manifest: any run longer than five minutes failed
  whichever resources were still waiting, furthest from the actual slowness.
  Waiting for dependencies is no longer bounded at all
- **This can fail runs that used to pass.** The default is still five minutes,
  and it now applies to each resource's own work, so a resource that legitimately
  takes longer, a large download or a clone of a big repository, needs
  `WithTimeout`, `SetResourceTimeout` or `--resource-timeout`. A resource cannot
  be cancelled, so an overrunning one is abandoned rather than stopped: the run
  reports it as failed, stops starting anything new, and whatever the operation
  was part way through is left as it is
- A dependency cycle fails before the run starts, naming the resources in the
  cycle, rather than being waited out
- `Package` and `User` no longer serialise against each other through a single
  global lock. Package work still holds the passwd lock as well as the package
  one, because maintainer scripts call `adduser` and `groupadd`
- The `DistUpgrade`, `AptAutoremove`, `AptHold` and `InstallDeb` helpers pass
  their arguments to the program rather than building a shell command, so
  `InstallDeb("/tmp/my pkg.deb")` works
- `Git` uses `go-git/go-git/v5` instead of the unmaintained `src-d/go-git.v4`

### Fixed

- A run could exit non-zero while reporting an empty list of failed resources.
  The dependency check recorded an error without setting a status, and the two
  disagreed. Whether a run failed and what gets reported now come from the same
  place
- The `User` resource looked the user up twice, so a user removed externally
  mid-run failed the second lookup
- `Template` no longer reaches into the `File` resource's internals to write its
  output. Both go through the same helper
- An apt signing key fetch that returns an HTTP error is an error. Without
  `curl -f` the 404 body went through `gpg --dearmor` and the error page was
  installed as the keyring, then trusted on every later run
- `WithLock` did not clear a lock key the resource already had, so escalating a
  `Package`, `User` or `Group` to the global lock was a silent noop

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
