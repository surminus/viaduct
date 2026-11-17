package viaduct

import (
	"context"
	"errors"
	"slices"
	"sync"
)

// LockKey names a domain that resources serialise on. A resource only
// contends with other resources sharing its key, so package work and passwd
// work can run at the same time. An empty key serialises against every other
// lock holder.
type LockKey string

// Lock keys for the domains that the standard resources serialise on.
const (
	// PackageLock is for anything that takes the package manager lock, such
	// as apt-get, dpkg, dnf or pacman.
	PackageLock LockKey = "package"

	// PasswdLock is for anything that writes the passwd or group databases,
	// such as useradd, usermod or groupadd.
	PasswdLock LockKey = "passwd"

	// FstabLock is for anything that formats a device, edits /etc/fstab or
	// changes what is mounted, such as Filesystem, Mount and Swap.
	FstabLock LockKey = "fstab"

	// NetworkLock is for anything that changes the host's network
	// identity or configuration, such as Hostname.
	NetworkLock LockKey = "network"
)

// errLockHeld is returned by Lock for a resource that already holds a lock.
var errLockHeld = errors.New("the resource already holds a lock")

// errResourceFinished is returned by Lock once the resource's attempt has
// finished, such as one that was abandoned while still running.
var errResourceFinished = errors.New("the resource has finished")

// Lock takes a lock part way through a resource's Run, and returns the
// function that releases it. It is the lock ResourceParams asks for, so the
// resource waits for others holding key, and an empty key excludes every
// other lock holder. The lock is released when the resource finishes if
// unlock has not been called. Waiting for the lock counts towards the
// resource's timeout. A resource that already holds a lock, from its
// ResourceParams or an earlier call, gets an error. Outside Apply, Lock takes
// nothing.
func Lock(ctx context.Context, key LockKey) (unlock func(), err error) {
	h, _ := ctx.Value(heldLockKey{}).(*heldLock)
	if h == nil {
		return func() {}, nil
	}

	return h.take(ctx, key)
}

type heldLockKey struct{}

// heldLock is the lock one attempt at a resource holds, from its params or
// from Lock. Locks are not reentrant, so a second one is refused rather
// than left to deadlock.
type heldLock struct {
	set *lockSet

	// check is called once the lock is taken, and an error releases it
	// again. The run uses it to stop anything starting after a resource
	// was abandoned, as it does for a lock from ResourceParams.
	check func() error

	mu      sync.Mutex
	holding bool
	done    bool
	release func()
}

func (h *heldLock) take(ctx context.Context, key LockKey) (func(), error) {
	h.mu.Lock()
	if h.done {
		h.mu.Unlock()
		return nil, errResourceFinished
	}

	if h.holding {
		h.mu.Unlock()
		return nil, errLockHeld
	}
	h.holding = true
	h.mu.Unlock()

	release, err := h.set.acquireContext(ctx, key)

	h.mu.Lock()
	defer h.mu.Unlock()

	if err != nil {
		h.holding = false
		return nil, err
	}

	// The attempt may have finished, such as by being abandoned, while this
	// waited. Nothing would release the lock after that.
	if h.done {
		release()
		return nil, errResourceFinished
	}

	if h.check != nil {
		if err := h.check(); err != nil {
			release()
			h.holding = false

			return nil, err
		}
	}

	h.release = release

	return h.unlock, nil
}

func (h *heldLock) unlock() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.release != nil {
		h.release()
		h.release = nil
		h.holding = false
	}
}

// finish releases a lock taken with Lock and stops any more being taken.
func (h *heldLock) finish() {
	h.mu.Lock()
	h.done = true
	h.mu.Unlock()

	h.unlock()
}

// lockDomains lists the keys a lock key covers, for domains that are not
// actually independent of each other.
//
// Package maintainer scripts routinely call adduser and groupadd, which take
// the same passwd and group locks as the User and Group resources, so a package
// resource holds the passwd lock as well as the package one. Otherwise a
// package install running alongside a User resource fails with "cannot lock
// /etc/passwd, try again later".
var lockDomains = map[LockKey][]LockKey{
	PackageLock: {PackageLock, PasswdLock},
}

// keysFor returns every key a lock key covers, in a stable order so that
// holders acquiring overlapping sets cannot deadlock against each other.
func keysFor(key LockKey) []LockKey {
	keys, ok := lockDomains[key]
	if !ok {
		return []LockKey{key}
	}

	out := make([]LockKey, len(keys))
	copy(out, keys)
	slices.Sort(out)

	return out
}

// lockSet holds the locks that serialise resources against each other during
// a run.
//
// A resource with a lock key only contends with other resources using the
// same key. A resource that asks for a lock without giving a key is
// serialised against everything, which is the safe default for a custom
// resource that hasn't said what it contends with.
type lockSet struct {
	// global is held for writing by keyless lock holders, and for reading by
	// keyed lock holders, so a keyless holder excludes every key while keyed
	// holders only queue behind their own key.
	global sync.RWMutex

	mu    sync.Mutex
	keyed map[LockKey]*sync.Mutex
}

func newLockSet() *lockSet {
	return &lockSet{keyed: make(map[LockKey]*sync.Mutex)}
}

// acquire takes the locks for a key, returning the function that releases them.
// An empty key takes the keyless lock, which excludes all other lock holders. A
// key that covers other domains takes those too, see lockDomains.
func (l *lockSet) acquire(key LockKey) func() {
	if key == "" {
		l.global.Lock()
		return l.global.Unlock
	}

	l.global.RLock()

	keys := keysFor(key)
	held := make([]*sync.Mutex, 0, len(keys))

	for _, k := range keys {
		m := l.keyMutex(k)
		m.Lock()
		held = append(held, m)
	}

	return func() {
		for i := len(held) - 1; i >= 0; i-- {
			held[i].Unlock()
		}

		l.global.RUnlock()
	}
}

// acquireContext is acquire that gives up when ctx is done. The locks are
// then released as soon as they are taken.
func (l *lockSet) acquireContext(ctx context.Context, key LockKey) (func(), error) {
	got := make(chan func(), 1)

	go func() { got <- l.acquire(key) }()

	select {
	case release := <-got:
		return release, nil
	case <-ctx.Done():
		go func() { (<-got)() }()
		return nil, ctx.Err()
	}
}

func (l *lockSet) keyMutex(key LockKey) *sync.Mutex {
	l.mu.Lock()
	defer l.mu.Unlock()

	if m, ok := l.keyed[key]; ok {
		return m
	}

	m := &sync.Mutex{}
	l.keyed[key] = m

	return m
}
