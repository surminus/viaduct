package viaduct

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetName(t *testing.T) {
	t.Parallel()

	m := New()
	d := Dir("test")
	r := m.Add(d)
	m.SetName(r, "test-name")

	expected := map[ResourceID]Resource{
		ResourceID("test-name"): {
			Attributes:   d,
			ResourceID:   "test-name",
			Status:       Pending,
			ResourceKind: ResourceKind("Directory"),
		},
	}

	assert.Equal(t, m.resources, expected)
}

func TestSetDep(t *testing.T) {
	t.Parallel()

	m := New()
	d := Dir("test")
	r := m.Add(d)
	m.SetDep(r, "test-dep")

	expected := map[ResourceID]Resource{
		r.ResourceID: {
			Attributes:   d,
			DependsOn:    []ResourceID{"test-dep"},
			ResourceID:   r.ResourceID,
			Status:       Pending,
			ResourceKind: ResourceKind("Directory"),
		},
	}

	assert.Equal(t, expected, m.resources)
}

func TestWithLock(t *testing.T) {
	t.Parallel()

	m := New()
	d := Dir("test")
	r := m.Add(d)
	m.WithLock(r)

	expected := map[ResourceID]Resource{
		r.ResourceID: {
			Attributes:   d,
			GlobalLock:   true,
			ResourceID:   r.ResourceID,
			Status:       Pending,
			ResourceKind: ResourceKind("Directory"),
		},
	}

	assert.Equal(t, expected, m.resources)
}

func TestAddResource(t *testing.T) {
	t.Parallel()

	t.Run("basic", func(t *testing.T) {
		t.Parallel()

		m := New()
		d := Dir("test")
		r, err := newResource([]*Resource{{ResourceID: "test"}})
		if err != nil {
			t.Fatal(err)
		}

		err = r.setKind(d)
		assert.NoError(t, err)

		err = m.addResource(r, d)
		assert.NoError(t, err)

		assert.Equal(t, 1, len(m.resources))

		for id, res := range m.resources {
			// Set ResourceKind happens separately to this function,
			// but should it?
			expected := Resource{
				Attributes:   d,
				DependsOn:    []ResourceID{ResourceID("test")},
				ResourceID:   id,
				Status:       Pending,
				ResourceKind: "Directory",
			}

			assert.Equal(t, expected, res)
		}
	})

	t.Run("error if resource repeated", func(t *testing.T) {
		t.Parallel()

		m := New()
		d := Dir("test")
		r, err := newResource([]*Resource{{ResourceID: "test"}})
		if err != nil {
			t.Fatal(err)
		}

		err = r.setKind(d)
		assert.NoError(t, err)

		err = m.addResource(r, d)
		assert.NoError(t, err)

		d2 := Dir("test")
		r2, err := newResource([]*Resource{{ResourceID: "test"}})
		if err != nil {
			t.Fatal(err)
		}

		err = m.addResource(r2, d2)
		assert.Error(t, err)
	})

	t.Run("automatic global lock", func(t *testing.T) {
		t.Parallel()

		m := New()
		p := Pkg("test")
		r, err := newResource([]*Resource{{ResourceID: "test"}})
		if err != nil {
			t.Fatal(err)
		}

		err = r.setKind(p)
		assert.NoError(t, err)

		err = m.addResource(r, p)
		assert.NoError(t, err)

		for id, res := range m.resources {
			expected := Resource{
				Attributes:   p,
				DependsOn:    []ResourceID{ResourceID("test")},
				GlobalLock:   true,
				ResourceID:   id,
				Status:       Pending,
				ResourceKind: ResourceKind("Package"),
			}

			assert.Equal(t, expected, res)
		}
	})
}
