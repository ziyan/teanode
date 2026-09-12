package browser

import (
	"fmt"
	"net"
)

type netNetwork = net.IPNet

func mustNetwork(cidr string) *net.IPNet {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	return network
}

func errorf(format string, arguments ...any) error {
	return fmt.Errorf(format, arguments...)
}
