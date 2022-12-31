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

        // The first argument is what operation to perform, such as
        // creating or deleting a file, and the second argument are
        // the attributes of the resource
        m.Add(&resources.Directory{"/tmp/test"})
        m.Add(&resources.File{Path: "/tmp/test/foo"})
}
```

All resources will run concurrently, so in this example we will declare a
dependency so that the directory is created before the file:

```go
func main() {
        m := viaduct.New()

        dir := m.Add(&resources.Directory{"/tmp/test"})
        m.Add(&resources.File{Path: "/tmp/test/foo"}, dir)
}
```

When you've added all the resources you need, we can apply them:

```go
func main() {
        m := viaduct.New()

        dir := m.Add(&resources.Directory{"/tmp/test"})
        m.Add(&resources.File{Path: "/tmp/test/foo"}, dir)

        m.Run()
}
```

Compile the package and run it:
```bash
go build -o viaduct
./viaduct
```

## CLI

The compiled binary comes with runtime flags:
```bash
./viaduct --help
```

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

        template := resources.NewTemplate(
                templates,
                "templates/test.txt",
                struct{ Name string }{Name: "Bella"},
        )

        // CreateFile is a helper function that takes two arguments
        m.Add(resources.CreateFile("test/foo", template))
}
```

The `EmbeddedFile` function works in a similar way, but without variables.

## Attributes

Like any good configuration management tool, we also have access to node
attributes under the `Attribute` variable:

```go
import (
        "fmt"

        "github.com/surminus/viaduct"
        "github.com/surminus/viaduct/resources"
)

func main() {
        m := viaduct.New()

        // E is an alias for creating an Execute resource
        m.Add(resources.Exec(fmt.Sprintf("echo \"Hello %s!\"", viaduct.Attribute.User.Username)))
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
        viaduct.Attribute.SetUser("laura")
        m := viaduct.New()

        // Will print my home directory
        m.Add(resources.Echo(viaduct.Attribute.User.Homedir))

        m.Run()
}
```
