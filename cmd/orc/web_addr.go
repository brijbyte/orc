package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

func resolveWebAddr(addr string, flagPort int, portSet bool) (string, error) {
	host, _, err := net.SplitHostPort(addr)
	if err == nil && !portSet {
		return addr, nil
	}
	if err != nil {
		host = strings.TrimSuffix(strings.TrimPrefix(addr, "["), "]")
	}
	port := flagPort
	if !portSet {
		if value := os.Getenv("PORT"); value != "" {
			var err error
			port, err = strconv.Atoi(value)
			if err != nil {
				return "", fmt.Errorf("invalid PORT %q", value)
			}
		}
	}
	if port < 0 || port > 65535 {
		return "", fmt.Errorf("invalid port %d", port)
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}
