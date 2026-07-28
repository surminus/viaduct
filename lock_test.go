package viaduct

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLockSet(t *testing.T) {
	t.Parallel()

	t.Run("the same key is serialised", func(t *testing.T) {
		t.Parallel()

		locks := newLockSet()

		release := locks.acquire(PackageLock)

		var acquired bool
		done := make(chan struct{})

		go func() {
			defer close(done)

			r := locks.acquire(PackageLock)
			acquired = true
			r()
		}()

		// The second holder cannot get in until the first lets go
		time.Sleep(20 * time.Millisecond)
		assert.False(t, acquired)

		release()
		<-done
		assert.True(t, acquired)
	})

	t.Run("unrelated keys run in parallel", func(t *testing.T) {
		t.Parallel()

		locks := newLockSet()

		release := locks.acquire("alpha")
		defer release()

		done := make(chan struct{})
		go func() {
			defer close(done)

			r := locks.acquire("beta")
			r()
		}()

		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("an unrelated lock key had to wait")
		}
	})

	t.Run("the package lock covers the passwd lock", func(t *testing.T) {
		t.Parallel()

		// Package maintainer scripts call adduser, so the two domains are not
		// independent and must not run at the same time
		for _, keys := range [][2]string{
			{PackageLock, PasswdLock},
			{PasswdLock, PackageLock},
		} {
			locks := newLockSet()

			release := locks.acquire(keys[0])

			var acquired bool
			done := make(chan struct{})

			go func() {
				defer close(done)

				r := locks.acquire(keys[1])
				acquired = true
				r()
			}()

			time.Sleep(20 * time.Millisecond)
			assert.False(t, acquired, "%s did not exclude %s", keys[0], keys[1])

			release()
			<-done
			assert.True(t, acquired)
		}
	})

	t.Run("a keyless lock excludes every key", func(t *testing.T) {
		t.Parallel()

		locks := newLockSet()

		release := locks.acquire("")

		var acquired bool
		done := make(chan struct{})

		go func() {
			defer close(done)

			r := locks.acquire(PackageLock)
			acquired = true
			r()
		}()

		time.Sleep(20 * time.Millisecond)
		assert.False(t, acquired)

		release()
		<-done
		assert.True(t, acquired)
	})

	t.Run("a key excludes a keyless lock", func(t *testing.T) {
		t.Parallel()

		locks := newLockSet()

		release := locks.acquire(PasswdLock)

		var acquired bool
		done := make(chan struct{})

		go func() {
			defer close(done)

			r := locks.acquire("")
			acquired = true
			r()
		}()

		time.Sleep(20 * time.Millisecond)
		assert.False(t, acquired)

		release()
		<-done
		assert.True(t, acquired)
	})

	t.Run("holders of the same key take turns", func(t *testing.T) {
		t.Parallel()

		locks := newLockSet()

		var (
			wg      sync.WaitGroup
			mu      sync.Mutex
			holding int
			most    int
		)

		for range 20 {
			wg.Go(func() {
				release := locks.acquire(PackageLock)
				defer release()

				mu.Lock()
				holding++
				if holding > most {
					most = holding
				}
				mu.Unlock()

				time.Sleep(time.Millisecond)

				mu.Lock()
				holding--
				mu.Unlock()
			})
		}

		wg.Wait()
		assert.Equal(t, 1, most)
	})
}
