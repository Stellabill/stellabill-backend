package outbox

import (
	"hash/fnv"
	"sort"
	"sync"
)

// ConsistentHashRing maps tenant IDs to partition numbers using consistent
// hashing with virtual nodes. This minimizes reshuffling when the partition
// count changes: resizing from N to N+1 only moves approximately 1/N of
// tenants to a new partition, preserving ordering within each tenant since
// the same tenant always maps to the same partition between resizes.
type ConsistentHashRing struct {
	mu         sync.RWMutex
	ring       map[uint32]int // hash position -> partition number
	sorted     []uint32       // sorted hash positions on the ring
	partitions int            // total number of partitions
	replicas   int            // virtual nodes per partition
}

// NewConsistentHashRing creates a ring that maps keys to partition numbers
// in [0, partitions). replicas controls how many virtual nodes each partition
// gets on the ring; more replicas gives a more uniform distribution but costs
// more memory. A typical value is 150.
func NewConsistentHashRing(partitions, replicas int) *ConsistentHashRing {
	if partitions <= 0 {
		partitions = 1
	}
	if replicas <= 0 {
		replicas = 150
	}

	r := &ConsistentHashRing{
		ring:       make(map[uint32]int, partitions*replicas),
		partitions: partitions,
		replicas:   replicas,
	}

	for p := 0; p < partitions; p++ {
		r.addPartition(p)
	}
	sort.Slice(r.sorted, func(i, j int) bool {
		return r.sorted[i] < r.sorted[j]
	})

	return r
}

func (r *ConsistentHashRing) addPartition(partition int) {
	for i := 0; i < r.replicas; i++ {
		h := fnv.New32a()
		key := make([]byte, 0, 24)
		key = appendUint32(key, uint32(partition))
		key = appendUint32(key, uint32(i))
		h.Write(key)
		hash := h.Sum32()
		r.ring[hash] = partition
		r.sorted = append(r.sorted, hash)
	}
}

func appendUint32(buf []byte, v uint32) []byte {
	return append(buf, byte(v>>24), byte(v>>16), byte(v>>8), byte(v))
}

// GetPartition returns the partition number for the given tenant ID.
// The mapping is deterministic: the same tenant ID always returns the
// same partition as long as the ring is not recreated with a different
// partition count.
func (r *ConsistentHashRing) GetPartition(tenantID string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.sorted) == 0 {
		return 0
	}

	h := fnv.New32a()
	h.Write([]byte(tenantID))
	hash := h.Sum32()

	idx := sort.Search(len(r.sorted), func(i int) bool {
		return r.sorted[i] >= hash
	})
	if idx >= len(r.sorted) {
		idx = 0
	}

	return r.ring[r.sorted[idx]]
}

// Partitions returns all partition numbers in the ring, sorted ascending.
func (r *ConsistentHashRing) Partitions() []int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[int]struct{}, r.partitions)
	for _, p := range r.ring {
		seen[p] = struct{}{}
	}

	result := make([]int, 0, len(seen))
	for p := range seen {
		result = append(result, p)
	}
	sort.Ints(result)
	return result
}

// NumPartitions returns the number of partitions in the ring.
func (r *ConsistentHashRing) NumPartitions() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.partitions
}
