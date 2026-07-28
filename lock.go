package viaduct

import (
	"slices"
	"sync"
)

// Lock keys for the domains that the standard resources serialise on. A
// resource only contends with other resources sharing its key, so package
// work and passwd work can run at the same time.
const (
	// PackageLock is for anything that takes the package manager lock, such
	// as apt-get, dpkg, dnf or pacman.
	PackageLock = "package"

	// PasswdLock is for anything that writes the passwd or group databases,
	// such as useradd, usermod or groupadd.
	PasswdLock = "passwd"
)

// lockDomains lists the keys a lock key covers, for domains that are not
// actually independent of each other.
//
// Package maintainer scripts routinely call adduser and groupadd, which take
// the same passwd and group locks as the User and Group resources, so a package
// resource holds the passwd lock as well as the package one. Otherwise a
// package install running alongside a User resource fails with "cannot lock
// /etc/passwd, try again later".
var lockDomains = map[string][]string{
	PackageLock: {PackageLock, PasswdLock},
}

// keysFor returns every key a lock key covers, in a stable order so that
// holders acquiring overlapping sets cannot deadlock against each other.
func keysFor(key string) []string {
	keys, ok := lockDomains[key]
	if !ok {
		return []string{key}
	}

	out := make([]string, len(keys))
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
	keyed map[string]*sync.Mutex
}

func newLockSet() *lockSet {
	return &lockSet{keyed: make(map[string]*sync.Mutex)}
}

// acquire takes the locks for a key, returning the function that releases them.
// An empty key takes the keyless lock, which excludes all other lock holders. A
// key that covers other domains takes those too, see lockDomains.
func (l *lockSet) acquire(key string) func() {
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

func (l *lockSet) keyMutex(key string) *sync.Mutex {
	l.mu.Lock()
	defer l.mu.Unlock()

	if m, ok := l.keyed[key]; ok {
		return m
	}

	m := &sync.Mutex{}
	l.keyed[key] = m

	return m
}
