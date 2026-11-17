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

Create a project in `main.go` and create a new manifest. A standard set of
resources are found in the
[resources](https://pkg.go.dev/github.com/surminus/viaduct/resources) package.

All resources will run concurrently, so in this example we will declare a
dependency so that the directory is created before the file:

```go
import (
        "github.com/surminus/viaduct"
        "github.com/surminus/viaduct/resources"
)

func main() {
        m := viaduct.New()

        dir := m.Add(&resources.Directory{Path: "/tmp/test"})
        m.Add(&resources.File{Path: "/tmp/test/foo"}, dir)

        m.Run()
}
```

When you have a linear sequence where each resource depends on the previous
one, you can use `Chain` rather than declaring each dependency by hand:

```go
m.Chain(
        &resources.Directory{Path: "/tmp/test"},
        &resources.File{Path: "/tmp/test/foo"},
        &resources.Link{Path: "/tmp/test/bar", Source: "/tmp/test/foo"},
)
```

Compile the package and run it:
```bash
go build -o viaduct
./viaduct
```

See the example in the [examples](examples/basic) directory.

## CLI

The compiled binary comes with runtime flags, such as `--dry-run`:
```bash
./viaduct --help
```

If your program has flags of its own, use `Apply` rather than `Run`. It
returns a report instead of printing and exiting.

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
//go:embed templates
var templates embed.FS

func main() {
        m := viaduct.New()

        template := viaduct.Must(resources.NewTemplate(
                templates,
                "templates/test.txt",
                struct{ Name string }{Name: "Bella"},
        ))

        m.Add(resources.CreateFile("test/foo", template))
}
```

The `EmbeddedFile` function works in a similar way, but without variables.

## Attributes

Like any good configuration management tool, we also have access to node
attributes with `Attributes()`:

```go
m.Add(resources.Echo("Hello " + viaduct.Attributes().User.Username))
```

## Sudo support

If you require to perform actions that require sudo access, such as using the
`Package` resource, or creating files using `File`, then you should run the
executable using `sudo`.

Otherwise, assigning permissions should be achieved by explicitly setting the
user and group in the resource.

Alternatively, you can set a default user attribute:
```go
func main() {
        if err := viaduct.Attributes().SetUser("laura"); err != nil {
                log.Fatal(err)
        }

        m := viaduct.New()

        // Will print my home directory
        m.Add(resources.Echo(viaduct.Attributes().User.HomeDir))

        m.Run()
}
```

## Using custom resources

Custom resources just need to implement the
[`ResourceAttributes`](https://pkg.go.dev/github.com/surminus/viaduct#ResourceAttributes)
interface, and can be tested with the
[`viaducttest`](https://pkg.go.dev/github.com/surminus/viaduct/viaducttest)
package.

See the example custom resource in the
[examples](examples/custom-resource/example.go) directory.
