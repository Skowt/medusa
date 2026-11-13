package process

import (
	"reflect"
	"testing"
)

func TestPortAllocator_AllocatePort(t *testing.T) {
	p := NewPortAllocator(6200, 10)

	// First allocation
	port1 := p.AllocatePort("/workspace1")
	if port1 != 6200 {
		t.Errorf("First allocation = %d, want 6200", port1)
	}

	// Second allocation
	port2 := p.AllocatePort("/workspace2")
	if port2 != 6210 {
		t.Errorf("Second allocation = %d, want 6210", port2)
	}

	// Same workspace should return same port
	port1Again := p.AllocatePort("/workspace1")
	if port1Again != port1 {
		t.Errorf("Same workspace returned different port: %d != %d", port1Again, port1)
	}
}

func TestPortAllocator_GetPort(t *testing.T) {
	p := NewPortAllocator(6200, 10)

	// Before allocation
	_, ok := p.GetPort("/workspace1")
	if ok {
		t.Error("GetPort should return false for unallocated workspace")
	}

	// After allocation
	p.AllocatePort("/workspace1")
	port, ok := p.GetPort("/workspace1")
	if !ok {
		t.Error("GetPort should return true for allocated workspace")
	}
	if port != 6200 {
		t.Errorf("GetPort = %d, want 6200", port)
	}
}

func TestPortAllocator_ReleasePort(t *testing.T) {
	p := NewPortAllocator(6200, 10)

	p.AllocatePort("/workspace1")
	p.ReleasePort("/workspace1")

	_, ok := p.GetPort("/workspace1")
	if ok {
		t.Error("GetPort should return false after release")
	}
}

func TestPortAllocator_PortRange(t *testing.T) {
	p := NewPortAllocator(6200, 10)

	port, rangeEnd := p.PortRange("/workspace1")
	if port != 6200 {
		t.Errorf("port = %d, want 6200", port)
	}
	if rangeEnd != 6209 {
		t.Errorf("rangeEnd = %d, want 6209", rangeEnd)
	}
}

func TestPortAllocator_ConcurrentAccess(t *testing.T) {
	p := NewPortAllocator(6200, 10)

	// Run concurrent allocations
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			p.AllocatePort("/workspace" + string(rune('0'+n)))
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify no crashes occurred (mutex protection)
}

func TestPortAllocator_AllocationSkipsSeededRanges(t *testing.T) {
	tests := []struct {
		name           string
		seeds          []PortSeed
		expectedOutput int
	}{
		{name: "first slot taken", seeds: []PortSeed{{Key: "a", Port: 6200}}, expectedOutput: 6210},
		{name: "gap reused", seeds: []PortSeed{{Key: "a", Port: 6210}}, expectedOutput: 6200},
		{name: "off-grid seed blocks both slots it overlaps", seeds: []PortSeed{{Key: "a", Port: 6205}}, expectedOutput: 6220},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPortAllocator(6200, 10)
			p.Seed(tt.seeds)
			if got := p.AllocatePort("new"); got != tt.expectedOutput {
				t.Errorf("AllocatePort(new) = %d, want %d", got, tt.expectedOutput)
			}
		})
	}
}

// A restart builds a fresh allocator that something may touch before the
// workspaces load; the persisted ports must still win, and a seed that
// collides with an earlier one must be refused so its workspace is reassigned.
func TestPortAllocator_Seed(t *testing.T) {
	tests := []struct {
		name             string
		allocatedFirst   string
		seeds            []PortSeed
		expectedRejected []string
		expectedPorts    map[string]int
	}{
		{
			name:           "persisted port evicts an earlier in-memory allocation",
			allocatedFirst: "early",
			seeds:          []PortSeed{{Key: "a", Port: 6200}, {Key: "b", Port: 6210}},
			expectedPorts:  map[string]int{"a": 6200, "b": 6210, "early": 6220},
		},
		{
			name:             "colliding seed is refused",
			seeds:            []PortSeed{{Key: "a", Port: 6200}, {Key: "b", Port: 6200}},
			expectedRejected: []string{"b"},
			expectedPorts:    map[string]int{"a": 6200, "b": 6210},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewPortAllocator(6200, 10)
			if tt.allocatedFirst != "" {
				p.AllocatePort(tt.allocatedFirst)
			}
			if got := p.Seed(tt.seeds); !reflect.DeepEqual(got, tt.expectedRejected) {
				t.Errorf("Seed() rejected = %v, want %v", got, tt.expectedRejected)
			}
			for key, want := range tt.expectedPorts {
				if got := p.AllocatePort(key); got != want {
					t.Errorf("AllocatePort(%s) = %d, want %d", key, got, want)
				}
			}
		})
	}
}
