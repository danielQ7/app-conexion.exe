//go:build windows

package main

import (
	"context"
	"net"
	"syscall"
)

// soExclusiveAddrUse impide que otro programa escuche en el mismo puerto a la vez
// (Windows lo permite por defecto con algunos programas, como AnyDesk).
const soExclusiveAddrUse = ^0x4 // SO_EXCLUSIVEADDRUSE = ~SO_REUSEADDR

func escucharExclusivo(dir string) (net.Listener, error) {
	lc := net.ListenConfig{
		Control: func(_, _ string, c syscall.RawConn) error {
			var errOpt error
			err := c.Control(func(fd uintptr) {
				errOpt = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, soExclusiveAddrUse, 1)
			})
			if err != nil {
				return err
			}
			return errOpt
		},
	}
	return lc.Listen(context.Background(), "tcp", dir)
}
