package viaduct

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetName(t *testing.T) {
	t.Parallel()

	m := New()
	d := D("test")
	r := m.Create(d)
	m.SetName(r, "test-name")

	expected := map[ResourceID]Resource{
		ResourceID("test-name"): {
			Attributes:   d,
			Operation:    OperationCreate,
			ResourceID:   "test-name",
			ResourceKind: KindDirectory,
			Status:       StatusPending,
		},
	}

	assert.Equal(t, m.resources, expected)
}

func TestSetDep(t *testing.T) {
	t.Parallel()

	m := New()
	d := D("test")
	r := m.Create(d)
	m.SetDep(r, "test-dep")

	expected := map[ResourceID]Resource{
		r.ResourceID: {
			Attributes:   d,
			DependsOn:    []ResourceID{"test-dep"},
			Operation:    OperationCreate,
			ResourceID:   r.ResourceID,
			ResourceKind: KindDirectory,
			Status:       StatusPending,
		},
	}

	assert.Equal(t, expected, m.resources)
}

func TestWithLock(t *testing.T) {
	t.Parallel()

	m := New()
	d := D("test")
	r := m.Create(d)
	m.WithLock(r)

	expected := map[ResourceID]Resource{
		r.ResourceID: {
			Attributes:   d,
			GlobalLock:   true,
			Operation:    OperationCreate,
			ResourceID:   r.ResourceID,
			ResourceKind: KindDirectory,
			Status:       StatusPending,
		},
	}

	assert.Equal(t, expected, m.resources)
}

func TestAddResource(t *testing.T) {
	t.Parallel()

	t.Run("basic", func(t *testing.T) {
		t.Parallel()

		m := New()
		d := D("test")
		r, err := newResource(OperationCreate, []*Resource{{ResourceID: "test"}})
		if err != nil {
			t.Fatal(err)
		}

		err = m.addResource(r, d)
		assert.NoError(t, err)

		assert.Equal(t, 1, len(m.resources))

		for id, res := range m.resources {
			// Set ResourceKind happens separately to this function,
			// but should it?
			expected := Resource{
				Attributes: d,
				DependsOn:  []ResourceID{ResourceID("test")},
				Operation:  OperationCreate,
				ResourceID: id,
				Status:     StatusPending,
			}

			assert.Equal(t, expected, res)
		}
	})

	t.Run("error if resource repeated", func(t *testing.T) {
		t.Parallel()

		m := New()
		d := D("test")
		r, err := newResource(OperationCreate, []*Resource{{ResourceID: "test"}})
		if err != nil {
			t.Fatal(err)
		}

		err = m.addResource(r, d)
		assert.NoError(t, err)

		d2 := D("test")
		r2, err := newResource(OperationCreate, []*Resource{{ResourceID: "test"}})
		if err != nil {
			t.Fatal(err)
		}

		err = m.addResource(r2, d2)
		assert.Error(t, err)
	})

	t.Run("automatic global lock", func(t *testing.T) {
		t.Parallel()

		m := New()
		p := P("test")
		r, err := newResource(OperationCreate, []*Resource{{ResourceID: "test"}})
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
				Operation:    OperationCreate,
				ResourceID:   id,
				ResourceKind: KindPackage,
				Status:       StatusPending,
			}

			assert.Equal(t, expected, res)
		}
	})
}

func TestCreateResource(t *testing.T) {
	t.Parallel()

	t.Run("basic", func(t *testing.T) {
		t.Parallel()

		m := New()
		d := D("test")
		r, err := m.createResource(d)
		assert.NoError(t, err)

		expected := Resource{
			Attributes:   D("test"),
			Operation:    OperationCreate,
			ResourceID:   r.ResourceID,
			ResourceKind: KindDirectory,
			Status:       StatusPending,
		}

		assert.Equal(t, &expected, r)
	})

	t.Run("operation not allowed", func(t *testing.T) {
		t.Parallel()

		m := New()
		// We cannot create an execution
		e := E("test")
		_, err := m.createResource(e)
		assert.Error(t, err)
	})
}

func TestDelete(t *testing.T) {
	t.Parallel()

	t.Run("basic", func(t *testing.T) {
		t.Parallel()

		m := New()
		d := D("test")
		r, err := m.deleteResource(d)
		assert.NoError(t, err)

		expected := Resource{
			Attributes:   D("test"),
			Operation:    OperationDelete,
			ResourceID:   r.ResourceID,
			ResourceKind: KindDirectory,
			Status:       StatusPending,
		}

		assert.Equal(t, &expected, r)
	})

	t.Run("operation not allowed", func(t *testing.T) {
		t.Parallel()

		m := New()
		// We cannot delete an execution
		e := E("test")
		_, err := m.deleteResource(e)
		assert.Error(t, err)
	})
}

func TestRun(t *testing.T) {
	t.Parallel()

	t.Run("basic", func(t *testing.T) {
		t.Parallel()

		m := New()
		e := E("test")
		r, err := m.runResource(e)
		assert.NoError(t, err)

		expected := Resource{
			Attributes:   E("test"),
			Operation:    OperationRun,
			ResourceID:   r.ResourceID,
			ResourceKind: KindExecute,
			Status:       StatusPending,
		}

		assert.Equal(t, &expected, r)
	})
}

func TestUpdate(t *testing.T) {
	t.Parallel()

	t.Run("basic", func(t *testing.T) {
		t.Parallel()

		m := New()
		a := NewAptUpdate()
		r, err := m.updateResource(a)
		assert.NoError(t, err)

		expected := Resource{
			Attributes:   Apt{},
			GlobalLock:   true,
			Operation:    OperationUpdate,
			ResourceID:   r.ResourceID,
			ResourceKind: KindApt,
			Status:       StatusPending,
		}

		assert.Equal(t, &expected, r)
	})
}
