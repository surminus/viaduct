# Viaduct

## Comments and documentation

Doc comments on exported types, fields and functions are for the end user.
Say what it does, what the default is, and anything that has to be set
alongside it. Nothing else.

Leave out the context that led to the code existing: the bug that prompted
it, the environment it was hit in, or the workaround it replaces. That
belongs in the commit message or the changelog, not in the API docs.

Good:

```go
// NoRecursive applies the ownership to the directory itself, leaving
// whatever is inside it alone. The default is to apply it to the whole
// tree.
NoRecursive bool
```

Bad:

```go
// NoRecursive applies the ownership to the directory itself and leaves
// everything inside it alone. By default the ownership is applied to the
// whole tree, which is wrong when the directory is only a container for
// paths owned by something else, and fails outright when one of those paths
// cannot be chowned, such as a read-only bind mount created by Docker.
NoRecursive bool
```

Comments on unexported code are different: explaining why something is done
a particular way is useful there, because the reader is working on the code
rather than using it.

## Commits

Explain why the change is being made. The diff already says what changed,
so the message is where the reason lives: what was wrong before, or what
became possible afterwards.

Keep it short. A sentence or two, or a single paragraph. The title is
`area: description`.

Add a `Co-authored-by` trailer whenever an AI agent wrote the commit.
