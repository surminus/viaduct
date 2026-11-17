# Viaduct [![CI](https://github.com/surminus/viaduct/actions/workflows/ci.yaml/badge.svg)](https://github.com/surminus/viaduct/actions/workflows/ci.yaml) [![Go Reference](https://pkg.go.dev/badge/github.com/surminus/viaduct.svg)](https://pkg.go.dev/github.com/surminus/viaduct)

A configuration management framework written in Go.

The framework allows you to write configuration in plain Go, which you would
then compile and distribute as a binary to the target machines.

This means that you don't need to bootstrap an instance with configuration
files or a runtime environment (eg "install chef"): simply download the binary,
and run it!

I'm using Viaduct to set up my personal development environment at
[surminus/myduct](https://github.com/surminus/myduct).

## Getting started

Create a project in `main.go` and create a new manifest:

```go
import (
        "github.com/surminus/viaduct"
)

func main() {
        // m is our manifest object
        m := viaduct.New()
}
```

A standard set of resources are found in the
[resources](https://pkg.go.dev/github.com/surminus/viaduct/resources) package.

To add them:

```go
import (
        "github.com/surminus/viaduct"
        "github.com/surminus/viaduct/resources"
)

func main() {
        m := viaduct.New()

        m.Add(&resources.Directory{Path: "/tmp/test"})
        m.Add(&resources.File{Path: "/tmp/test/foo"})
}
```

All resources will run concurrently, so in this example we will declare a
dependency so that the directory is created before the file:

```go
func main() {
        m := viaduct.New()

        dir := m.Add(&resources.Directory{Path: "/tmp/test"})
        m.Add(&resources.File{Path: "/tmp/test/foo"}, dir)
}
```

When you have a linear sequence where each resource depends on the previous
one, you can wire the whole thing in a single call with `Chain` rather than
declaring each dependency by hand:

```go
func main() {
        m := viaduct.New()

        // The file waits for the directory, and the symlink waits for the file
        m.Chain(
                &resources.Directory{Path: "/tmp/test"},
                &resources.File{Path: "/tmp/test/foo"},
                &resources.Link{Path: "/tmp/test/bar", Source: "/tmp/test/foo"},
        )
}
```

`Chain` returns the created resources in order, so you can still branch other
resources off any individual link. The returned value also has `Last` and
`First` helpers for safely grabbing the ends of the chain to depend on.

To start a chain from a resource that already exists, use `ChainFrom`:

```go
func main() {
        m := viaduct.New()

        base := m.Add(&resources.Directory{Path: "/tmp/test"})

        // The file depends on base, and the link depends on the file
        chain := m.ChainFrom(base,
                &resources.File{Path: "/tmp/test/foo"},
                &resources.Link{Path: "/tmp/test/bar", Source: "/tmp/test/foo"},
        )

        // This runs only once the whole chain above has completed
        m.Add(&resources.File{Path: "/tmp/test/done"}, chain.Last())
}
```

`ChainTo` is the mirror of `ChainFrom`: it makes an existing resource run after
the chain by wiring it onto the chain's last link for you.

When you've added all the resources you need, we can apply them:

```go
func main() {
        m := viaduct.New()

        dir := m.Add(&resources.Directory{Path: "/tmp/test"})
        m.Add(&resources.File{Path: "/tmp/test/foo"}, dir)

        m.Run()
}
```

Compile the package and run it:
```bash
go build -o viaduct
./viaduct
```

See the example in the [examples](examples/basic) directory.

## Resources

The [resources](https://pkg.go.dev/github.com/surminus/viaduct/resources)
package covers the common building blocks:

- `File`, `Directory` and `Link` for files, directories and symlinks
- `Template` for rendering Go templates from disk to a file
- `Line` for editing individual lines in a file that isn't fully managed
- `Package` and `Apt` for installing packages and managing apt repositories
- `User` and `Group` for users and groups, and `Service` for systemd units
- `Archive` for extracting tar and zip archives
- `Sysctl` for writing and applying kernel parameters
- `Download` and `Git` for fetching files and cloning repositories
- `Execute` for running arbitrary commands, and `Shell` for multi-line scripts

Most resources have shortcut constructors, such as `resources.Dir`,
`resources.Pkg` and `resources.SystemUser`. See the package docs for the full
set.

## CLI

The compiled binary comes with runtime flags:
```bash
./viaduct --help
```

## Using viaduct as a library

A program with flags of its own, or one that wants to handle the outcome
itself, uses `Apply` instead of `Run`. Nothing happens when viaduct is
imported: flags are only read by `Run`, and the system attributes are read
when first used.

```go
report, err := m.Apply(ctx, viaduct.Options{
        Env:     viaduct.Env{DryRun: dryRun},
        Handler: handler,
        OnResult: func(r viaduct.Result) {
                // Called as each resource finishes, never concurrently
        },
})
```

`Apply` returns an error wrapping `ErrInvalidManifest`, `ErrPreflightFailed` or
`ErrRunFailed`, and a `Report` with a `Result` for every resource, including
the one that failed. Each result has the resource's ID, kind, operation and
status, whether it changed anything, its error, and how long it took. A
resource that did not run because a dependency failed has that dependency
as its `Cause`.
Cancelling the context stops resources that are running and stops the rest
from starting.

Everything a run reports is an `Event`, sent to the `Handler` in `Options`.
`NewTextHandler` prints what `Run` prints. Command output arrives as events
too, one per line.

Use `AddNamed` to give a resource a stable ID of your own, rather than one
generated from its attributes.

### Retries and outcomes

```go
ready := m.AddNamed("ready", resources.Exec("curl -fsS http://localhost:8080/ready"))
m.WithRetry(ready, viaduct.RetryPolicy{Attempts: 5, Initial: time.Second, Max: 30 * time.Second})

// A failure here is reported, but does not fail the run. Anything depending
// on it does not run
m.AllowFailure(m.Add(resources.Exec("optional-tuning")))
```

A retried resource releases its lock while it waits, and an error wrapped with
`viaduct.Permanent` stops the retries. A resource can return
`viaduct.Skip(reason)` when it has nothing to do, and anything depending on it
still runs.

## Embedded files and templates

There are helper functions to allow us to use the
[`embed`](https://pkg.go.dev/embed) package to flexibly work with files and
templates.

To create a template, first create a file in `templates/test.txt` using Go
[`template`](https://pkg.go.dev/text/template) syntax:

```bash
My cat is called {{ .Name }}
```

We can then generate the data to create our file:

```go
import (
        "embed"

        "github.com/surminus/viaduct"
        "github.com/surminus/viaduct/resources"
)

//go:embed templates
var templates embed.FS

func main() {
        m := viaduct.New()

        // Must panics if the template cannot be read or rendered, which
        // for an embedded template is a bug in the program
        template := viaduct.Must(resources.NewTemplate(
                templates,
                "templates/test.txt",
                struct{ Name string }{Name: "Bella"},
        ))

        // CreateFile is a helper function that takes two arguments
        m.Add(resources.CreateFile("test/foo", template))
}
```

The `EmbeddedFile` function works in a similar way, but without variables.
Both return an error rather than exiting, so they can also be handled like any
other error.

## Attributes

Like any good configuration management tool, we also have access to node
attributes through the `Attributes` function. They are read from the system on
first use, and `LoadAttributes` returns the same value along with any error
from reading them:

```go
import (
        "fmt"

        "github.com/surminus/viaduct"
        "github.com/surminus/viaduct/resources"
)

func main() {
        m := viaduct.New()

        // E is an alias for creating an Execute resource
        m.Add(resources.Exec(fmt.Sprintf("echo \"Hello %s!\"", viaduct.Attributes().User.Username)))
}
```

## Sudo support

If you require to perform actions that require sudo access, such as using the
`Package` resource, or creating files using `File`, then you should run the
executible using `sudo`.

Otherwise, assigning permissions should be achieved by explicitly setting the
user and group in the resource.

Alternatively, you can set a default user attribute:
```go
func main() {
        viaduct.Attributes().SetUser("laura")
        m := viaduct.New()

        // Will print my home directory
        m.Add(resources.Echo(viaduct.Attributes().User.HomeDir))

        m.Run()
}
```

## Using custom resources

Custom resources just need to implement the
[`ResourceAttributes`](https://pkg.go.dev/github.com/surminus/viaduct#ResourceAttributes)
interface.

See the example custom resource in the
[examples](examples/custom-resource/example.go) directory.
