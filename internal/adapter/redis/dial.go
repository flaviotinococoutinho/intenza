package redis

import (
	"net"
	"time"
)

func netDialer(timeout time.Duration) net.Dialer {
	return net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
}
