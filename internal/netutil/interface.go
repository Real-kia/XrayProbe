// Package netutil provides small, dependency-free network helpers shared by
// the CLI's onboarding commands.
package netutil

import (
	"errors"
	"net"
)

// DefaultInterfaceName returns the name of the network interface the
// operating system would use for outbound internet traffic right now, so a
// caller doesn't have to run ifconfig/ip a and read it off by hand.
//
// It works by opening a UDP "connection" to a public address and reading
// back which local address the kernel's routing table chose for it, then
// matching that address to one of the host's interfaces. UDP dial does not
// send any packets on its own - this never touches the network.
func DefaultInterfaceName() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", err
	}
	defer conn.Close()

	localAddr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return "", errors.New("could not determine the local address used for the default route")
	}

	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if ok && ipNet.IP.Equal(localAddr.IP) {
				return iface.Name, nil
			}
		}
	}
	return "", errors.New("no local interface matched the default route's address")
}
