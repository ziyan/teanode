package tools

import "sync"

// The registry: every tool package says what it offers from its own init,
// through a factory, and the catalog is whatever registered, in the order
// the packages were imported. A factory rather than a value, so a package
// whose tools carry state can make them fresh for every catalog.

var registry struct {
	mutex     sync.Mutex
	factories []func() []*Tool
}

// Register adds a factory. Called from a tool package's init.
func Register(factory func() []*Tool) {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	registry.factories = append(registry.factories, factory)
}

// Build is a catalog of everything registered, in registration order.
func Build() *Catalog {
	registry.mutex.Lock()
	factories := append([]func() []*Tool(nil), registry.factories...)
	registry.mutex.Unlock()
	catalog := NewCatalog()
	for _, factory := range factories {
		for _, tool := range factory() {
			catalog.Register(tool)
		}
	}
	return catalog
}

// Reset forgets every factory, for a test that registers its own.
func Reset() {
	registry.mutex.Lock()
	defer registry.mutex.Unlock()
	registry.factories = nil
}
