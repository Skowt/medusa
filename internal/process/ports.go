package process

import (
	"sync"
)

// PortAllocator manages port allocation for workspaces. Keys are workspace
// IDs (see PortRange).
type PortAllocator struct {
	mu        sync.Mutex
	portStart int
	rangeSize int
	allocated map[string]int // workspace ID -> port base
}

// PortSeed is a port base already persisted on a workspace record.
type PortSeed struct {
	Key  string
	Port int
}

// NewPortAllocator creates a new port allocator
func NewPortAllocator(start, rangeSize int) *PortAllocator {
	return &PortAllocator{
		portStart: start,
		rangeSize: rangeSize,
		allocated: make(map[string]int),
	}
}

// AllocatePort allocates a port range for a workspace
func (p *PortAllocator) AllocatePort(key string) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	if port, ok := p.allocated[key]; ok {
		return port
	}
	port := p.firstFreeLocked()
	p.allocated[key] = port
	return port
}

// firstFreeLocked returns the lowest range base, stepping from portStart,
// whose range overlaps no allocated range. Allocated bases need not sit on
// that grid: a port persisted under an older portStart still blocks every
// candidate it overlaps.
func (p *PortAllocator) firstFreeLocked() int {
	for candidate := p.portStart; ; candidate += p.rangeSize {
		if !p.takenLocked(candidate) {
			return candidate
		}
	}
}

func (p *PortAllocator) takenLocked(port int) bool {
	for _, base := range p.allocated {
		if p.overlaps(port, base) {
			return true
		}
	}
	return false
}

// overlaps reports whether the ranges starting at a and b share a port.
func (p *PortAllocator) overlaps(a, b int) bool {
	return a < b+p.rangeSize && b < a+p.rangeSize
}

// Seed adopts persisted port bases, in order, so a workspace keeps the port its
// services were started on across medusa restarts. A seed wins over an
// in-memory allocation it collides with (a key that has since gone away still
// holds the port); a seed colliding with an earlier seed is refused and its
// key is returned, so the caller can allocate that workspace afresh.
func (p *PortAllocator) Seed(seeds []PortSeed) (rejected []string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	seeded := make(map[string]bool, len(seeds))
	for _, s := range seeds {
		if s.Key == "" || s.Port <= 0 {
			continue
		}
		if p.collidesWithSeedLocked(s, seeded) {
			rejected = append(rejected, s.Key)
			continue
		}
		p.evictCollisionsLocked(s)
		p.allocated[s.Key] = s.Port
		seeded[s.Key] = true
	}
	return rejected
}

func (p *PortAllocator) collidesWithSeedLocked(s PortSeed, seeded map[string]bool) bool {
	for key := range seeded {
		if key != s.Key && p.overlaps(s.Port, p.allocated[key]) {
			return true
		}
	}
	return false
}

func (p *PortAllocator) evictCollisionsLocked(s PortSeed) {
	for key, base := range p.allocated {
		if key != s.Key && p.overlaps(s.Port, base) {
			delete(p.allocated, key)
		}
	}
}

// GetPort returns the allocated port for a workspace
func (p *PortAllocator) GetPort(key string) (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	port, ok := p.allocated[key]
	return port, ok
}

// ReleasePort releases the port allocation for a workspace
func (p *PortAllocator) ReleasePort(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	delete(p.allocated, key)
}

// PortRange returns the port and range size for a workspace. The key is the
// workspace's ID rather than its root: workspaces that own no worktree share
// the source repo as their root, and handing two of them the same range means
// their run scripts fight over the same port.
func (p *PortAllocator) PortRange(workspaceID string) (port int, rangeEnd int) {
	port = p.AllocatePort(workspaceID)
	return port, port + p.rangeSize - 1
}
