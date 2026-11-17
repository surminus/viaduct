package viaduct

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		for _, keys := range [][2]LockKey{
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

// lockingResource runs fn as its Run
type lockingResource struct {
	testResourceType
	fn func(ctx context.Context) error
}

func (r *lockingResource) Run(ctx context.Context, _ *Logger) error {
	return r.fn(ctx)
}

func TestLock(t *testing.T) {
	t.Parallel()

	t.Run("takes nothing outside Apply", func(t *testing.T) {
		t.Parallel()

		unlock, err := Lock(context.Background(), FstabLock)
		require.NoError(t, err)
		unlock()
	})

	t.Run("excludes resources holding the same key", func(t *testing.T) {
		t.Parallel()

		locked := make(chan struct{})
		release := make(chan struct{})

		var mu sync.Mutex
		var order []string

		record := func(s string) {
			mu.Lock()
			defer mu.Unlock()
			order = append(order, s)
		}

		holder := &lockingResource{testResourceType: testResourceType{Value: "holder"}}
		holder.fn = func(ctx context.Context) error {
			unlock, err := Lock(ctx, FstabLock)
			if err != nil {
				return err
			}
			defer unlock()

			close(locked)
			<-release
			record("holder")

			return nil
		}

		waiter := &lockingResource{testResourceType: testResourceType{Value: "waiter", LockKey: FstabLock}}
		waiter.fn = func(context.Context) error {
			record("waiter")
			return nil
		}

		gate := &lockingResource{testResourceType: testResourceType{Value: "gate"}}
		gate.fn = func(context.Context) error {
			<-locked
			return nil
		}

		m := New()
		m.Add(holder)
		m.Add(waiter, m.Add(gate))

		go func() {
			<-locked
			time.Sleep(20 * time.Millisecond)
			close(release)
		}()

		_, err := m.Apply(context.Background(), Options{})
		require.NoError(t, err)

		assert.Equal(t, []string{"holder", "waiter"}, order)
	})

	t.Run("refuses a second lock", func(t *testing.T) {
		t.Parallel()

		r := &lockingResource{testResourceType: testResourceType{Value: "r", LockKey: PackageLock}}
		r.fn = func(ctx context.Context) error {
			_, err := Lock(ctx, FstabLock)
			return err
		}

		m := New()
		res := m.Add(r)

		report, err := m.Apply(context.Background(), Options{})
		require.ErrorIs(t, err, ErrRunFailed)
		assert.ErrorIs(t, byID(report)[res.ResourceID].Err, errLockHeld)
	})

	t.Run("is released when the resource finishes", func(t *testing.T) {
		t.Parallel()

		first := &lockingResource{testResourceType: testResourceType{Value: "first"}}
		first.fn = func(ctx context.Context) error {
			_, err := Lock(ctx, FstabLock)
			return err
		}

		second := &lockingResource{testResourceType: testResourceType{Value: "second"}}
		second.fn = func(ctx context.Context) error {
			unlock, err := Lock(ctx, FstabLock)
			if err != nil {
				return err
			}

			unlock()

			return nil
		}

		m := New()
		m.Add(second, m.Add(first))

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err := m.Apply(ctx, Options{})
		require.NoError(t, err)
	})

	t.Run("is refused once the run has abandoned a resource", func(t *testing.T) {
		t.Parallel()

		locks := newLockSet()
		stop := errors.New("abandoned")

		h := &heldLock{set: locks, check: func() error { return stop }}
		_, err := h.take(context.Background(), FstabLock)
		require.ErrorIs(t, err, stop)

		release := locks.acquire(FstabLock)
		release()
	})

	t.Run("stops waiting when the resource is cancelled", func(t *testing.T) {
		t.Parallel()

		locks := newLockSet()
		release := locks.acquire(FstabLock)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		h := &heldLock{set: locks}
		_, err := h.take(ctx, FstabLock)
		require.ErrorIs(t, err, context.Canceled)

		release()

		unlock, err := h.take(context.Background(), FstabLock)
		require.NoError(t, err, "a failed wait leaves nothing held")
		unlock()
	})
}
