# Viaduct [![CI](https://github.com/surminus/viaduct/actions/workflows/ci.yaml/badge.svg)](https://github.com/surminus/viaduct/actions/workflows/ci.yaml) [![Go Reference](https://pkg.go.dev/badge/github.com/surminus/viaduct.svg)](https://pkg.go.dev/github.com/surminus/viaduct)

A configuration management framework written in Go.

The framework allows you to write configuration in plain Go, which you would
then compile and distribute as a binary to the target machines.

This means that you don't need to bootstrap an instance with configuration
files or a runtime environment (eg "install chef"): simply download the binary,
and run it!

### v2

I'm currently working on adding concurrency support with resource dependency
management, and hope to merge this in soon! It should however work alongside
the scripted style syntax described here, so should not break anything.

## Getting started

Create a project in `main.go` and import the framework:

```
import (
        v "github.com/surminus/viaduct" // By convention we use "v"
)

func main() {}
```

Add resources as part of the `main()` function. To create a directory and file:

```
func main() {
        dir := v.Directory{Path: "test"}.Create()
        v.File{Path: fmt.Sprintf("%s/foo", dir.Path), Content: "bar"}.Create()
}
```

Resources always return their resource object. In the example above, we use
the `Directory` path as part of the file creation.

This also means we can run whatever action we need to on that resource:

```
func main() {
        dir := viaduct.Directory{Path: "test"}.Create()
        v.File{Path: fmt.Sprintf("%s/foo", dir.Path), Content: "bar"}.Create()

        dir.Delete()
}
```

I'm using Viaduct to set up my personal development environment at
[surminus/myduct](https://github.com/surminus/myduct).

### Embedded files and templates

There are helper functions to allow us to use the
[`embed`](https://pkg.go.dev/embed) package to flexibly work with files and
templates.

To create a template, first create a file in `templates/test.txt` using Go
[`template`](https://pkg.go.dev/text/template) syntax:

```
My name is {{ .Name }}
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
        template := v.NewTemplate(
                templates,
                "templates/test.txt",
                struct{ Name string }{Name: "Bella"},
        )

        v.File{Path: "test/foo", Content: template}.Create()
}
```

The `EmbeddedFile` function works in a similar way, but without variables.

### Attributes

Like any good configuration management tool, we also have access to node
attributes under the `Attribute` variable:

```
import (
        "fmt"

        v "github.com/surminus/viaduct"
)

func main() {
        fmt.Println(v.Attribute.User.HomeDir) // Prints my home directory
}
```

### Sudo support

If you require to perform actions that require sudo access, such as using the
`Package` resource, or creating files using `File`, then you should run the
executible using `sudo`.

Otherwise, assigning permissions should be achieved by explicitly setting the
user and group.

Alternatively, you can set a default user attribute:
```
v.Attribute.SetUser("laura")
```

For resources that you wish to run as `root`, you can set the `Root` option:
```
v.File{
    Path: "foo",
    Root: true,
}
```
