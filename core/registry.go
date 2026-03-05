package core

import "sync"

// DriverFactory creates a FileSystem from a config map.
type DriverFactory func(config map[string]interface{}) (FileSystem, error)

var (
	driverFactories = map[string]DriverFactory{}
	registryMu      sync.RWMutex
)

// RegisterDriver registers a named driver factory.
func RegisterDriver(name string, factory DriverFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	driverFactories[name] = factory
}

// GetDriverFactory returns a driver factory by name, or nil if not found.
func GetDriverFactory(name string) DriverFactory {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return driverFactories[name]
}
