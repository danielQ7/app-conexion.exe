//go:build !windows

package main

import "net"

func escucharExclusivo(dir string) (net.Listener, error) {
	return net.Listen("tcp", dir)
}
