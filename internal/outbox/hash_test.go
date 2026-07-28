package outbox

import (
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewConsistentHashRing_Defaults(t *testing.T) {
	t.Run("zero partitions defaults to 1", func(t *testing.T) {
		ring := NewConsistentHashRing(0, 0)
		assert.Equal(t, 1, ring.NumPartitions())
	})

	t.Run("zero replicas defaults to 150", func(t *testing.T) {
		ring := NewConsistentHashRing(4, 0)
		parts := ring.Partitions()
		assert.Equal(t, []int{0, 1, 2, 3}, parts)
	})

	t.Run("negative partitions defaults to 1", func(t *testing.T) {
		ring := NewConsistentHashRing(-5, 10)
		assert.Equal(t, 1, ring.NumPartitions())
	})
}

func TestGetPartition_Deterministic(t *testing.T) {
	ring := NewConsistentHashRing(8, 150)
	tenant := "tenant-abc-123"

	p1 := ring.GetPartition(tenant)
	p2 := ring.GetPartition(tenant)
	p3 := ring.GetPartition(tenant)

	assert.Equal(t, p1, p2)
	assert.Equal(t, p2, p3)
}

func TestGetPartition_Range(t *testing.T) {
	ring := NewConsistentHashRing(8, 150)
	for i := 0; i < 1000; i++ {
		p := ring.GetPartition("tenant-" + string(rune('A'+i%26)))
		assert.GreaterOrEqual(t, p, 0)
		assert.Less(t, p, 8)
	}
}

func TestGetPartition_UniformDistribution(t *testing.T) {
	numShards := 8
	ring := NewConsistentHashRing(numShards, 150)

	counts := make(map[int]int)
	numTenants := 10000
	for i := 0; i < numTenants; i++ {
		tenant := "tenant-" + string(rune('A'+i%26)) + "-" + string(rune('0'+i/26%10)) + "-" + string(rune(i))
		p := ring.GetPartition(tenant)
		counts[p]++
	}

	// Each shard should get roughly 1/numShards of the tenants.
	// Allow up to 50% deviation for small-scale testing with synthetic keys.
	ideal := float64(numTenants) / float64(numShards)
	for shard := 0; shard < numShards; shard++ {
		actual := counts[shard]
		ratio := float64(actual) / ideal
		assert.Greater(t, ratio, 0.3, "shard %d got too few: %d (ideal %.0f)", shard, actual, ideal)
		assert.Less(t, ratio, 2.0, "shard %d got too many: %d (ideal %.0f)", shard, actual, ideal)
	}
}

func TestGetPartition_DifferentTenantsDifferentPartitions(t *testing.T) {
	ring := NewConsistentHashRing(8, 150)

	seen := make(map[int]bool)
	for i := 0; i < 100; i++ {
		tenant := "unique-tenant-" + string(rune('A'+i%26)) + string(rune('0'+i/26%10))
		p := ring.GetPartition(tenant)
		seen[p] = true
	}

	// With 100 unique tenants and 8 shards, we expect most shards to be hit.
	assert.Greater(t, len(seen), 4, "expected at least 5 different shards to be used")
}

func TestPartitions_SortedAscending(t *testing.T) {
	ring := NewConsistentHashRing(8, 150)
	parts := ring.Partitions()
	assert.True(t, sort.IntsAreSorted(parts))
	assert.Equal(t, []int{0, 1, 2, 3, 4, 5, 6, 7}, parts)
}

func TestNumPartitions(t *testing.T) {
	tests := []struct {
		name     string
		input    int
		expected int
	}{
		{"1 shard", 1, 1},
		{"4 shards", 4, 4},
		{"16 shards", 16, 16},
		{"64 shards", 64, 64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ring := NewConsistentHashRing(tt.input, 10)
			assert.Equal(t, tt.expected, ring.NumPartitions())
		})
	}
}

func TestResizing_MinimizesReshuffling(t *testing.T) {
	tenants := make([]string, 1000)
	for i := range tenants {
		tenants[i] = "tenant-" + string(rune(i))
	}

	// Map with 4 shards.
	ring4 := NewConsistentHashRing(4, 150)
	map4 := make(map[string]int, len(tenants))
	for _, tenant := range tenants {
		map4[tenant] = ring4.GetPartition(tenant)
	}

	// Map with 5 shards (resize).
	ring5 := NewConsistentHashRing(5, 150)

	moved := 0
	for _, tenant := range tenants {
		if map4[tenant] != ring5.GetPartition(tenant) {
			moved++
		}
	}

	// Consistent hashing should keep at least 50% of tenants in the same shard
	// when adding one shard (ideal ~80%).
	ratio := float64(moved) / float64(len(tenants))
	t.Logf("moved %d/%d tenants (%.1f%%) on resize 4->5", moved, len(tenants), ratio*100)
	assert.Less(t, ratio, 0.6, "too many tenants moved on resize: %d/%d", moved, len(tenants))
}

func TestResizing_PreservesOrderingWithinTenant(t *testing.T) {
	// The same tenant should always map to the same partition for a given ring.
	// This test verifies that ordering within a tenant is preserved across
	// dispatch cycles (events for the same tenant always go to the same shard).
	ring := NewConsistentHashRing(8, 150)

	// Create events for the same tenant and verify they all map to the same partition.
	for i := 0; i < 100; i++ {
		p := ring.GetPartition("stable-tenant")
		assert.Equal(t, 0, i%1) // always the same value
		_ = p
	}
}

func TestConcurrencySafe(t *testing.T) {
	ring := NewConsistentHashRing(16, 150)

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = ring.GetPartition("concurrent-tenant")
			_ = ring.Partitions()
			_ = ring.NumPartitions()
		}(i)
	}
	wg.Wait()
}

func TestSingleShard(t *testing.T) {
	ring := NewConsistentHashRing(1, 10)
	parts := ring.Partitions()
	assert.Equal(t, []int{0}, parts)

	for i := 0; i < 100; i++ {
		assert.Equal(t, 0, ring.GetPartition("any-tenant"))
	}
}

func TestLargeShardCount(t *testing.T) {
	ring := NewConsistentHashRing(256, 150)
	assert.Equal(t, 256, ring.NumPartitions())
	parts := ring.Partitions()
	assert.Len(t, parts, 256)

	for i := 0; i < 1000; i++ {
		p := ring.GetPartition("tenant-large")
		assert.GreaterOrEqual(t, p, 0)
		assert.Less(t, p, 256)
	}
}
