package cache

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLRU_GetSetBasic(t *testing.T) {
	c := NewLRU[string, int](3)
	c.Set("a", 1)
	c.Set("b", 2)

	v, ok := c.Get("a")
	require.True(t, ok)
	assert.Equal(t, 1, v)

	_, ok = c.Get("missing")
	assert.False(t, ok)
}

func TestLRU_Len(t *testing.T) {
	c := NewLRU[string, int](5)
	assert.Equal(t, 0, c.Len())
	c.Set("x", 10)
	assert.Equal(t, 1, c.Len())
	c.Set("y", 20)
	assert.Equal(t, 2, c.Len())
}

func TestLRU_EvictionOrder(t *testing.T) {
	// maxSize=3: insert a,b,c → full. Access "a" to make it MRU.
	// Insert "d" → evicts "b" (LRU), not "a".
	c := NewLRU[string, int](3)
	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3)
	c.Get("a") // promote a to front
	c.Set("d", 4)

	_, ok := c.Get("b")
	assert.False(t, ok, "b should have been evicted")

	_, ok = c.Get("a")
	assert.True(t, ok, "a should still be present")

	_, ok = c.Get("c")
	assert.True(t, ok, "c should still be present")

	_, ok = c.Get("d")
	assert.True(t, ok, "d should be present")

	assert.Equal(t, 3, c.Len())
}

func TestLRU_UpdateExisting(t *testing.T) {
	c := NewLRU[string, int](3)
	c.Set("a", 1)
	c.Set("a", 99)
	v, ok := c.Get("a")
	require.True(t, ok)
	assert.Equal(t, 99, v)
	assert.Equal(t, 1, c.Len())
}

func TestLRU_Delete(t *testing.T) {
	c := NewLRU[string, int](3)
	c.Set("a", 1)
	c.Delete("a")
	_, ok := c.Get("a")
	assert.False(t, ok)
	assert.Equal(t, 0, c.Len())

	// Delete of absent key is a no-op.
	c.Delete("nonexistent")
}

func TestLRU_CapacityOne(t *testing.T) {
	c := NewLRU[int, string](1)
	c.Set(1, "first")
	c.Set(2, "second")
	_, ok := c.Get(1)
	assert.False(t, ok, "first should be evicted when capacity=1")
	v, ok := c.Get(2)
	require.True(t, ok)
	assert.Equal(t, "second", v)
}
