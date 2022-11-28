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

```
import (
        v "github.com/surminus/viaduct" // By convention we use "v"
)

func main() {
        m := v.New()
}
```

Add resources:

```
func main() {
        m := v.New()

        m.Create(v.Directory{"/tmp/test"})
        m.Create(v.File{Path: "/tmp/test/foo"})
}
```

All resources will run concurrently, so in this example we will declare a
dependency so that the directory is created before the file:

```
func main() {
        m := v.New()

        dir := m.Create(v.Directory{"/tmp/test"})
        m.Create(v.File{Path: "/tmp/test/foo"}, dir)
}
```

When you've added all the resources you need, we can apply them:

```
func main() {
        m := v.New()

        dir := m.Create(v.Directory{"/tmp/test"})
        m.Create(v.File{Path: "/tmp/test/foo"}, dir)

        m.Start()
}
```

Compile the package and run it:
```
go build -o viaduct
./viaduct
```

## CLI

The compiled binary comes with runtime flags:
```
./viaduct --help
```

## Operations and Resources

The primary operations are the `Create()` and `Delete()` functions which
perform creations (or updates) and deletions respectively, while the `Run()`
operation is used for the `Execute` resource.

## Embedded files and templates

There are helper functions to allow us to use the
[`embed`](https://pkg.go.dev/embed) package to flexibly work with files and
templates.

To create a template, first create a file in `templates/test.txt` using Go
[`template`](https://pkg.go.dev/text/template) syntax:

```
My cat is called {{ .Name }}
```

We can then generate the data to create our file:

```
import (
        "embed"

        v "github.com/surminus/viaduct"
)

//go:embed templates
var templates embed.FS

func main() {
        m := v.New()

        template := v.NewTemplate(
                templates,
                "templates/test.txt",
                struct{ Name string }{Name: "Bella"},
        )

        m.Create(v.File{Path: "test/foo", Content: template})
}
```

The `EmbeddedFile` function works in a similar way, but without variables.

## Attributes

Like any good configuration management tool, we also have access to node
attributes under the `Attribute` variable:

```
import (
        "fmt"

        v "github.com/surminus/viaduct"
)

func main() {
        m := v.New()

        // v.E is an alias for creating an Execute resource
        m.Run(v.E(fmt.Sprintf("echo \"Hello %s!\"", v.Attribute.User.Username)))
}
```

## Sudo support

If you require to perform actions that require sudo access, such as using the
`Package` resource, or creating files using `File`, then you should run the
executible using `sudo`.

Otherwise, assigning permissions should be achieved by explicitly setting the
user and group in the resource.

Alternatively, you can set a default user attribute:
```
func main() {
        v.Attribute.SetUser("laura")
        m := v.New()

        // Will print my home directory
        m.Run(v.E(fmt.Sprintf("echo %s", v.Attribute.User.Homedir)))
}
```

## Scripting mode

It's possible to use resources directly in scripting mode:

```
import (
        v "github.com/surminus/viaduct"
)

func main() {
        v.E("echo hello").Run()
        v.E("echo world").Run()
}
```

These will just run in order and without concurrency.
