package system_setting

import (
	"sync"
)

// DefaultGlobalProxySessionTTL is the default lifetime (in seconds) of a
// session identifier substituted into the global proxy URL via the "%s"
// placeholder. Within the lifetime of a session the same (channel, key) pair
// resolves to the same upstream IP, maximising the useful life of the key.
const DefaultGlobalProxySessionTTL = 3600

var (
	globalProxyMu            sync.RWMutex
	globalProxyURL           = ""
	globalProxySessionTTLSec = DefaultGlobalProxySessionTTL
)

// GetGlobalProxy returns the currently-configured global proxy URL. The URL
// may contain a literal "%s" placeholder; callers should use the proxy
// resolver helpers to substitute a session identifier before use.
func GetGlobalProxy() string {
	globalProxyMu.RLock()
	defer globalProxyMu.RUnlock()
	return globalProxyURL
}

// SetGlobalProxy updates the global proxy URL in memory. Persistence to the
// database is handled by the option subsystem.
func SetGlobalProxy(v string) {
	globalProxyMu.Lock()
	globalProxyURL = v
	globalProxyMu.Unlock()
}

// GetGlobalProxySessionTTL returns the session-identifier lifetime in seconds.
// Returns the default when the configured value is non-positive.
func GetGlobalProxySessionTTL() int {
	globalProxyMu.RLock()
	defer globalProxyMu.RUnlock()
	if globalProxySessionTTLSec <= 0 {
		return DefaultGlobalProxySessionTTL
	}
	return globalProxySessionTTLSec
}

// SetGlobalProxySessionTTL updates the session TTL in memory. Values <= 0 are
// treated as "use the default" at read time but still stored verbatim so the
// admin UI reflects exactly what was written.
func SetGlobalProxySessionTTL(v int) {
	globalProxyMu.Lock()
	globalProxySessionTTLSec = v
	globalProxyMu.Unlock()
}
