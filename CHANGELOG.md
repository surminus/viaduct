# Viaduct

A configuration management framework written in Go.

## v1.0.0 (unreleased)

### Upgrading from v0.8.0

A manifest that only uses `New`, `Add`, `Chain` and `Run` with the standard
resources needs no change. Beyond that:

- `viaduct.Attribute.X` becomes `viaduct.Attributes().X`
- `PreflightChecks(log *Logger)` and `Run(log *Logger)` on a custom resource
  take a context first:

  ```go
  func (r *MyResource) PreflightChecks(ctx context.Context, log *viaduct.Logger) error
  func (r *MyResource) Run(ctx context.Context, log *viaduct.Logger) error
  ```

- In `Run`, read dry run from `viaduct.EnvFrom(ctx).DryRun` rather than
  `viaduct.Cli.DryRun`. Check the current state first, log `Noop` and return
  when nothing needs doing, log the change with `Info`, and return before
  making it in a dry run. See [the example](examples/custom-resource)
- Run commands with `viaduct.Command{Name: ..., Args: ...}.Run(ctx, log)`
  rather than `os/exec`, and go through `EnvFrom(ctx).Path` for absolute
  paths, so the resource can be tested with `viaducttest`
- `NewLogger(kind, operation)` in tests becomes
  `viaduct.NewLogger(nil, kind, operation)`, or use `viaducttest.NewEnv`
- `Pending`, `Success`, `Failed` and `DependencyFailed` become
  `StatusPending`, `StatusSuccess`, `StatusFailed` and
  `StatusDependencyFailed`, and a lock key is a `viaduct.LockKey`
- The helpers that exited on error now return it, and the ones listed under
  Removed have standard library or `Command` replacements
- A program with flags of its own uses `Apply` rather than `Run`

### Added

- A new `Filesystem` resource, with a `Mkfs` shortcut, for creating a
  filesystem on an empty device. The device is probed with `blkid -p` first: a
  device with the same filesystem is left alone, and one with anything else on
  it, including a partition table, is a permanent error. It never formats a
  device that has something on it, or anything that is not a block device.
  `msdos`, `fat` and `vfat` count as the same type. It takes `FstabLock`
- A new `Mount` resource, which creates the mount point, mounts the path if
  it is not already, and only once the mount succeeds keeps one `/etc/fstab`
  entry for the path, running `systemctl daemon-reload` when the entry
  changes. A path already mounted from another source is an error, and so is
  a mount point that is itself a symlink. `UUID=` and `LABEL=` sources, and
  symlinked devices, are compared by the device they resolve to, and a bind
  mount by the directory it comes from. Part of the same device mounted at the
  path is a different source, unless `subvol` or `subvolid` selects it
- A new `Swap` resource, which runs `mkswap` on an empty device, never forced,
  records the swap in `/etc/fstab` by UUID, runs `systemctl daemon-reload`
  when the entry changes, and turns it on. An active swap missing from
  `/etc/fstab` is recorded, and one with a different priority is a warning,
  since changing it means turning the swap off. A device that is not a block
  device is an error
- A new `SysfsValue` resource for values under `/sys`, comparing the bracketed
  choice in files such as `/sys/kernel/mm/transparent_hugepage/enabled`
- A new `Hostname` resource, with a `SetHostname` shortcut, which sets the
  host name with `hostnamectl`, or only until the next boot with `Transient`.
  It does not edit `/etc/hosts`
- A new `TimeSync` resource, which makes sure chrony has synchronised the
  clock, waiting up to `MaxWait` if not. The clock is stepped first only when
  chrony is not synchronised or it is more than a second out, and never with
  `NoStep`
- A new `CopyDir` resource, which copies a directory tree, writing only the
  files, directories and symlinks that differ. Each entry takes its mode from
  `Source`, and entries in `Path` that are not in `Source` are left alone
- A new `KernelModule` resource, with a `LoadModule` shortcut, which loads a
  module with `modprobe` unless it is already loaded or built in, and with
  `Persist` loads it at boot through `/etc/modules-load.d`
- `FstabLock` and `NetworkLock`, alongside `PackageLock` and `PasswdLock`

- `Apply(ctx, Options)` runs a manifest and returns a `Report` with a `Result`
  for every resource, and an error, rather than printing and exiting. The
  error wraps `ErrInvalidManifest`, `ErrPreflightFailed` or `ErrRunFailed`.
  Cancelling the context stops resources that have not started from starting.
  `Run` is now a wrapper around it for command-line programs, and stops the
  run the same way on SIGINT or SIGTERM, exiting with 130 or 143. A manifest
  can be applied more than once, each time with its own report, but not
  concurrently
- `Must`, for using a function that returns an error inline when the error
  would be a bug, such as `viaduct.Must(resources.NewTemplate(...))`
- **Events and handlers.** Everything a run reports is an `Event`, with a
  time, a level (`LevelInfo`, `LevelOutput`, `LevelNoop`, `LevelChanged`,
  `LevelWarn`, `LevelError`), the resource ID, kind, operation and
  description, a message and fields. `Options.Handler` receives them, and is
  never called concurrently. `NewTextHandler` gives the output `Run` prints
- Command output is reported as `LevelOutput` events, one per line, with a
  `stream` field of `stdout` or `stderr`. `Logger.Writer` does the same for a
  custom resource's own commands. A line ends at `\n`, `\r` or `\r\n`, so
  progress output arrives a step at a time, and a line longer than 64 KiB is
  split. A background process left holding a
  command's output open no longer stops the resource finishing, but its
  output stops being collected two seconds after the command exits, and
  anything it writes after that fails. Redirect the output of a process that
  is meant to keep running, such as with `nohup ... >/var/log/x.log 2>&1 &`
- Each `Result` has `Changed`, which is true when the resource logged a change
  with `Info`. `Options.OnResult` is called with each result as the resource
  finishes, and never concurrently. `Report.Failed` returns the failures
- `AddNamed`, which adds a resource under a name rather than a generated ID
- `--json` output includes `changed` and `duration` for each resource
- **`Env`, carried in the context.** `viaduct.EnvFrom(ctx)` gives a resource
  `DryRun`, `Root` and `Runner`, set from `Options.Env`. `Root` is a directory
  every absolute path a resource reads or writes itself is taken to be under,
  including `/proc` and `/sys`, and `Runner` runs commands, so resources can be
  tested without touching the host. `Env.Path` applies the root, and
  `..` cannot climb above it. `Apply` refuses a `Root` without a `Runner`,
  since commands would run on the host
- `Command`, with `Run` and `Output`, runs a program through the `Runner`,
  logging its output, or discarding it with a nil `Logger`. `ExecRunner` runs
  each command in its own process group, which is sent SIGTERM when the
  context is cancelled, and SIGKILL if it has not stopped 30 seconds later. `ExitCode` reads the exit
  status from an error, and `ExitError` is the one a fake runner returns
- The `viaducttest` package, with a fake `Runner`, a `Recorder` handler and a
  test `Env` rooted in a temporary directory. `Env.BlockDevice` marks a path
  as a block device, for testing a resource that needs one. `Response.Times`
  limits a response to a number of commands, for an answer that changes part
  way through, `Runner.Otherwise` answers any command nothing else matches,
  and `Exit` is the error for an exit status returned from `Do`
- `ApplyOne`, which runs a resource's preflight checks and then the resource,
  so a custom resource can use others once it knows a value only available
  part way through a run
- `Lock`, which takes a lock part way through a resource, such as
  `FstabLock` around the steps of a custom resource that need it, rather than
  for its whole run
- `ResourceParams.Timeout`, for a resource that needs longer than the manifest
  allows, or a negative duration for none. `WithTimeout` and the
  `ResourceTimeout` option override it
- **Retries.** `WithRetry` tries a failed resource
  again following a `RetryPolicy`: a number of attempts, with exponential
  backoff and jitter between `Initial` and `Max`, and an optional `MaxElapsed`.
  The resource's lock is released while it waits. An error wrapped with
  `Permanent` is not retried, and nor is a resource abandoned after its
  timeout. One that stops when its timeout passes is retried. Each attempt
  has its own `Result`, with `Attempt` and `Final`, and `Report.Final`
  returns the last result of each resource
- `Skip(reason)`, returned by a resource with nothing to do, gives it the new
  `StatusSkipped` status. Anything depending on it runs
- `WriteFileAtomic`, which replaces a file through a temporary file in the
  same directory, synced and renamed, keeping its mode, including setuid,
  setgid and sticky bits, and its ownership. `WriteAtomic` does the same for
  content streamed by a function, and `ResolveSymlink` returns the file
  either of them writes. A symlink is followed, at the path, in a directory
  above it or in what another link points to, only when the effective user or
  root owns it. Running as root, a link another user made is refused.
  `Env.ResolveSymlink` resolves a path under `Env.Root`, so an absolute link
  there stays under it
- `IgnoreMissing` on `Sysctl`, for keys the kernel may not have, such as one
  from a module that is not loaded. They are written as `-key = value`, so
  neither viaduct nor systemd-sysctl at boot fails on them, and a missing one
  no longer makes the resource apply again on every run
- `AllowFailure` lets a resource fail without failing the run. The failure is in
  its result, marked `AllowFailure`, and logged as a warning. Resources that
  depend on it do not run: they are `StatusDependencyFailed`, and fail the run
  unless they are allowed to fail too
- `Result.Cause` names the resource whose failure stopped a
  `StatusDependencyFailed` resource from running, and `--json` reports it as
  `cause`
- `Event.DryRun` is set on every event in a dry run. `Run` tags changes that
  would have been made `DRY` rather than `OK`, and `--json` has
  `"dry_run": true`
- `DaemonReload` on `Service`, which runs `systemctl daemon-reload` first
  when systemd reports that the unit or its drop-ins changed on disk
- `User` and `Env` on `Execute`, and `Env` on `Shell`. `User` runs the command
  and its `Unless` guard as another user with `runuser`
- `PermissionsOnly` on `Directory`, as on `File`, which applies only the mode
  and ownership that are set to an existing directory, leaving what is inside
  it alone
- `Substitute` on `Line`, with a `Substitute` shortcut, which replaces the
  text `Match` matches within each line rather than the whole line, like
  sed's `s///g`
- `Source` on `File`, with a `CopyFile` shortcut, which copies the content
  of another file, read when the resource runs, so it can be one an earlier
  resource creates
- `Rename` on `Archive`, which extracts an entry to another path in `Dest`
- `Filesystem.UUID`, the UUID of the filesystem once the resource has run,
  for mounting it by UUID from a custom resource that runs it with `ApplyOne`
- `Latest` on `Package`, which upgrades installed packages that have a newer
  version available, on Debian derivatives. There, a name pinned to a version,
  such as `curl=8.5.0-1`, is a noop when that version is installed
- `OnChange` runs a resource only when one of its dependencies changed
  something in the run, such as a restart after its configuration file is
  written. Otherwise it is `StatusSkipped`, and its dependants still run
- `Group` labels resources as one named step. Each of their results and
  events carries the name as `Group`, and `--json` reports it as `group`, so
  a handler or `OnResult` can report the step as a whole. The resources run
  as they otherwise would
- `AllowAbsoluteLinks` on `Archive`, which extracts symlinks with an absolute
  target
- `PreserveOwners` on `Archive`, which gives each entry the owner and group
  the tar archive records. A directory that already exists keeps its owner
- `Response.Then` in `viaducttest`, which sets the answer a command gives
  once the previous answer has been used
- `Replace` on `File`, which replaces text in `Content` or `Source` before the
  file is written. With `Source` set to `Path`, it edits the file in place
- `OnGroupResult` in `Options`, and `Groups` in the report, which give one
  result for each group once every resource in it has finished
- `AptClean`, which removes the package files apt has downloaded
- `reload` as an `Action` on `Service`, and `ReloadService`. A service that
  is not running is left alone

### Changed

- **Nothing in viaduct exits the program apart from `Run`.** Building a
  manifest records its errors, such as a duplicate resource, a nil dependency
  or `SetName` on a resource from another manifest, and `Apply` returns them
  before anything runs. `Add` still returns a resource that can be depended on
- **Helpers return errors.** `FileSize`, `MatchChmod`, `MatchChown`,
  `SystemAttributes.SetUser`, `SystemAttributes.JSON`,
  `resources.EmbeddedFile` and `resources.NewTemplate` return an error rather
  than exiting.
  `Logger.Fatal` is gone. `SetUser` no longer logs
- **Mode and ownership are never set through a symlink.** `File`,
  `Template`, `Download`, `Directory` and `Git` change a file through a
  descriptor opened without following links, and a symlink at a path whose
  permissions are managed is an error. `File`, `Template` and `Download`
  follow a symlink to write the file, under the same rule as
  `WriteFileAtomic`, and set the permissions on the file it points to.
  Applying ownership recursively changes a symlink itself rather than what it
  points to
- A directory whose contents change while its ownership is applied
  recursively no longer fails when a file disappears part way through
- `ExpandPath` only expands `~` and `~/...`, and leaves every other path as it
  is
- **Lock keys have their own type, `LockKey`.** `PackageLock`, `PasswdLock`,
  `FstabLock` and `NetworkLock` are `LockKey` constants, and `WithLockKey`,
  `NewResourceParamsWithLockKey` and the `LockKey` fields take one. A
  custom key is written as `viaduct.LockKey("name")`
- **Status constants have a `Status` prefix.** `Pending`, `Success`, `Failed`
  and `DependencyFailed` are `StatusPending`, `StatusSuccess`, `StatusFailed`
  and `StatusDependencyFailed`. Their values, as they appear in `--json`, are
  unchanged
- **`Logger` is backed by a `Handler`.** `NewLogger(handler, kind, operation)`
  replaces `NewLogger`, `NewStandardLogger`, `NewQuietLogger` and
  `NewSilentLogger`, and its fields take any value rather than only strings.
  `Logger.Entries`, `Logger.Quiet`, `Logger.Silent`, `ResultCollector` and the
  package-level `Log` are gone
- **`Add` returns the resource the manifest holds.** `SetName` therefore
  renames the caller's `*Resource`, and resources that depend on it follow the
  new name
- Dependencies are resolved when the manifest is applied. A dependency on a
  resource from another manifest, or on a name that does not exist, is an
  error rather than being skipped, which used to lose the ordering silently
- Resources that differ only in what cannot be represented as JSON, such as
  unexported fields or a func, no longer collide. The second and later get a
  numbered suffix on their ID. An exact duplicate is still an error
- Every method that takes a resource records an error when the resource is not
  in the manifest, rather than doing nothing
- **`ResourceAttributes` takes a context.** `PreflightChecks` and `Run` are
  `PreflightChecks(ctx context.Context, log *Logger)` and
  `Run(ctx context.Context, log *Logger)`. A custom resource needs the new
  signatures, and reads dry run from `EnvFrom(ctx).DryRun` rather than
  `Cli.DryRun`
- **A timeout cancels the resource's context.** Commands are stopped, rather
  than left running while the run moves on. A resource that has not returned
  40 seconds after its context is cancelled is abandoned. SIGINT or
  SIGTERM cancels running resources too, and a second signal exits straight
  away
- **Package work has no timeout by default.** Stopping dpkg part way through
  leaves it interrupted, so `Package`, `Apt` when it runs `apt-get update`,
  and `Execute` or `Shell` with `LockKey` set to `PackageLock` (including
  `DistUpgrade`, `AptAutoremove`, `AptHold` and `InstallDeb`) run until they
  finish. `WithTimeout` and `--resource-timeout` still apply to them
- **A failed `Execute` or `Shell` reports no change.** They log the command
  once it has finished, or in a dry run, the command they would have run
- **Dry run checks everything and skips only the changes.** Resources compare
  the current state as usual, so a dry run reports what is already correct as
  a noop rather than every resource as changing. Read-only commands, such as
  `apt-mark showhold`, `dpkg-query`, `systemctl is-enabled` and `Unless`
  guards, run in a dry run
- **`Package` checks what is installed first.** It asks `dpkg-query`,
  `rpm -q` or `pacman -Q` which of the packages are there, installs only the
  missing ones, and uninstalls or purges only the ones present. A package
  left with only its configuration still counts for `Purge` on Debian
  derivatives. When nothing needs doing it is a noop rather than running the
  package manager and reporting a change. A name with a version, release or
  glob in it, such as `curl=8.5.0`, is always passed to the package manager
- **This stops `Package` upgrading packages that are already installed.**
  Installing an installed package used to upgrade it, when the package
  manager had a newer version. That package is now left as it is, unless
  `Latest` is set
- `InstallDeb` is a noop when the deb's package is installed at the deb's
  version, rather than running `dpkg -i` on every run
- **`Package`, `DistUpgrade`, `AptAutoremove` and `InstallDeb` never ask a
  question.** `apt-get` and `dpkg` run with `DEBIAN_FRONTEND=noninteractive`,
  so debconf takes each default. A configuration file that was edited locally
  is kept, and any other is replaced by the package's version, rather than
  dpkg prompting on the terminal
- **`Archive` extracts an archive once.** After extracting it writes a
  marker holding the archive's SHA-256, `Strip`, `Pick`, `Rename` and
  `PreserveOwners` under `/var/lib/viaduct/archive`, or
  `~/.local/state/viaduct/archive` when not running as root, and is a noop
  while the marker matches and `Dest` and the picked entries exist. A changed archive or changed options extract again
- **`Archive` fails on an entry it will not extract.** An entry whose name
  climbs out of `Dest` used to be skipped silently, and a symlink pointing
  outside `Dest` or a hardlink to an entry that was not extracted was skipped
  with a warning, leaving a partial extraction that succeeded. Each now fails
  the resource, even when `Pick` leaves the entry out. A symlink with an
  absolute target fails unless `AllowAbsoluteLinks` is set. Entries already
  extracted stay, and the next run extracts the archive again, even with
  `NotIfExists`
- A `Download` whose file already exists with a matching `Checksum` is a noop,
  rather than being downloaded again
- Downloads and Git clones and pulls stop when the context is cancelled
- **`Sysctl` applies only its own file**, with `sysctl -p <file>`, or
  `sysctl -e -p <file>` when `IgnoreMissing` is set. It used to run
  `sysctl --system`, which reapplied every file on the host and undid anything
  set since boot. A key the kernel does not have, and that is not in
  `IgnoreMissing`, is an error before anything is written or applied, and so
  is a value still not set once `sysctl` has run. A key
  with a `/` in it, such as `net.ipv4.conf.eth0/100.forwarding`, is read from
  the same place sysctl uses, with the `/` standing for a dot in one
  component, and a key that would point outside `/proc/sys` is an error
- **This can fail runs that used to pass.** `sysctl --system` exited cleanly
  when a key was missing, so a missing key was never reported, and the
  resource applied again on every run. A key from a module needs the module
  loaded before the resource runs, such as with a dependency on whatever
  loads it. List any key that may legitimately be missing in `IgnoreMissing`
- **`File`, `Template`, `Line`, `Sysctl` and `Apt` write files atomically.** A
  crash part way through a write leaves the old file or the new one, never a
  truncated mixture. The file is replaced rather than rewritten, so its inode
  changes, and a symlink is followed to the file it points to only when the
  effective user owns the link. A file that
  cannot be replaced, such as one bind mounted on its own or one in a
  directory that cannot be written to, is still written in place
- **`Download` keeps the existing file until the new one is verified.** It
  downloads to a temporary file next to the destination, checks the
  `Checksum`, and renames it into place. A failed, interrupted or mismatched
  download used to leave nothing behind, removing the file that was there. A
  symlink at `Path` is followed and kept, as with `WriteFileAtomic`
- `Download` compares `Checksum` without regard to case, and refuses a
  redirect from https to anything else

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
- **`--json` always prints a JSON document**, including when the run fails
  before any resource runs. It has `status` (`success`, `failed` or
  `interrupted`), `error` when the run failed, and the preflight failures in
  `failures`. An interrupted run has no `failures`
- `--silent` still prints the error when the run fails
- `--dump-manifest` writes to a new file in `$TMPDIR` readable only by the
  user, with each resource's `Status` and `Error` from the run, and prints
  its path, or with `--json` puts it in `manifest`. It used to write a
  predictable name under `/tmp` readable by everyone
- Failures name the resource by its description as well as its ID: in the
  failure tree, in `preflight-failed`, and in the error of a resource whose
  dependency failed. Adding the same resource twice is the error
  `<Kind> <Description> is already in the manifest; use AddNamed to add it
  under another name`
- The dry run warning is an event from `Apply` rather than a timestamped line
  printed by `Run`
- **The temporary directory is under `$TMPDIR` and is always removed.** It is
  created on first use by `TmpDir` or `TmpFile`, which now returns an error,
  and removed when `Run` finishes, whether or not the run failed. It used to be
  under `~/.viaduct/tmp` and was left behind by every failed run.
  `SystemAttributes.TmpDir` is gone

### Removed

- `RunCommand`, `SudoCommand`, `PrependSudo`, `CommandTrue`, `CommandFalse`
  and `CommandOutput`, which ran commands through bash on the host. Use
  `viaduct.Command`, which goes through the `Runner`, so it is logged, faked
  in tests and stopped on cancellation
- `ExpandPathRoot`. Use `filepath.Abs`
- `FileContents`. Use `os.ReadFile`
- `IsDirectory`. Use `DirExists`
- `Resource.Status`, `Resource.Error`, `Resource.Failed` and the `Error`
  type. The `Report` from `Apply` holds the outcome of a run

### Fixed

- `Apt` writes its `Parameters` in a stable order. With more than one, the
  order changed between runs, so the file was rewritten and `apt-get update`
  ran again on an otherwise unchanged host
- When `apt-get update` fails, `Apt` puts its sources file back as it was, so
  the next run sees the change and runs the update again. It used to keep the
  new file, and the next run found it up to date and skipped the update
- `Apt` fetches an https `SigningKeyURL` with curl restricted to https, so a
  redirect cannot fetch the key over http
- **`Archive` cannot write outside `Dest`.** A crafted archive could combine
  symlinks and hardlinks to overwrite, chmod and chown any file the run could
  write to. Every entry is now written through an `os.Root` opened on `Dest`,
  which refuses any path that resolves outside it. A file in the archive
  replaces what is at its path, rather than writing into it, so a symlink or
  hardlink there is never followed

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
