// Package multipathguard holds the one private activation guard of the multipath
// profile. A public request stays unsupported while it is unset, and only a test
// fixture inside this module sets it. R19 removes this package after certification.
package multipathguard

import "sync/atomic"

// Active lets the public constructors accept a multipath configuration.
var Active atomic.Bool
