//go:build windows

package lansync

import (
	"net"
	"syscall"
)

func setReuseAddr(fd uintptr) {
	_ = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
}

func setReusePort(fd uintptr) {}

func joinMulticastGroup(conn *net.UDPConn, ifi *net.Interface, maddr *net.UDPAddr) error {
	raw, err := conn.SyscallConn()
	if err != nil {
		return err
	}
	ip4 := maddr.IP.To4()
	if ip4 == nil {
		return nil
	}
	var mreq syscall.IPMreq
	copy(mreq.Multiaddr[:], ip4)
	if ifi != nil {
		if addrs, err := ifi.Addrs(); err == nil {
			for _, a := range addrs {
				if ipNet, ok := a.(*net.IPNet); ok {
					if lip := ipNet.IP.To4(); lip != nil {
						copy(mreq.Interface[:], lip)
						break
					}
				}
			}
		}
	}
	var setErr error
	_ = raw.Control(func(fd uintptr) {
		setErr = syscall.SetsockoptIPMreq(syscall.Handle(fd), syscall.IPPROTO_IP, syscall.IP_ADD_MEMBERSHIP, &mreq)
	})
	return setErr
}
