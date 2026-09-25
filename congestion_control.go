package quic

import "fmt"

// SetCongestionControlV1 selects the built-in sender for connections created with
// this configuration. name must be exactly "reno" or "bbrv3". The default is Reno.
// An invalid name or nil receiver returns an error without changing selection.
// Configure selection before Dial, Listen, or returning from GetConfigForClient;
// concurrent mutation is unsupported. Existing connections retain their selection.
func (c *Config) SetCongestionControlV1(name string) error {
	if c == nil {
		return fmt.Errorf("quic: nil congestion control configuration")
	}
	if name != "reno" && name != "bbrv3" {
		return fmt.Errorf("quic: unsupported congestion control %q", name)
	}
	c.congestionControl = name
	return nil
}

// CongestionControlV1 returns the selected built-in sender. An unset or nil
// configuration selects "reno".
func (c *Config) CongestionControlV1() string {
	if c == nil || c.congestionControl == "" {
		return "reno"
	}
	return c.congestionControl
}

// CongestionControlV1 returns this connection's immutable selected sender,
// including after migration or close. It is safe to call concurrently.
func (c *Conn) CongestionControlV1() string {
	return c.config.CongestionControlV1()
}
