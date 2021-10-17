# Viaduct [![CI](https://github.com/surminus/viaduct/actions/workflows/ci.yaml/badge.svg)](https://github.com/surminus/viaduct/actions/workflows/ci.yaml)

A configuration management framework written in Go.

The framework allows you to write configuration in plain Go, compiled and
distributed as a binary.

## Getting started

Create a project in `main.go` and import the framework:

```
import "github.com/surminus/viaduct"

func main() {
}
```

Add resources as part of the `main()` function. To create a directory and file:

```
func main() {
    dir := viaduct.Directory{Path: "test"}.Create()
    viaduct.File{Path: fmt.Sprintf("%s/foo", dir.Path), Content: "bar"}.Create()
}
```

In the example above, we are making use of the attributes of the previously
created `Directory` resource when we create the file.

Since the resource actions always return the resource object, we can easily
delete what we created:

```
func main() {
    dir := viaduct.Directory{Path: "test"}.Create()
    viaduct.File{Path: fmt.Sprintf("%s/foo", dir.Path), Content: "bar"}.Create()

    dir.Delete()
}
```

Like any good configuration management tool, we also have access to node
attributes under the `Attribute` variable:

```
import (
    "fmt"

    "github.com/surminus/viaduct"
)

func main() {
    dir := viaduct.Directory{Path: "test"}.Create()
    viaduct.File{Path: fmt.Sprintf("%s/foo", dir.Path), Content: "bar"}.Create()

    dir.Delete()

    fmt.Println(viaduct.Attribute.JSON())
}
```

When you're happy with your configuration, compile and run using `go build`.
