package main

// This file contains the HTTP CONNECT implementation of the proxy interface defined in proxy.go

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
)

type httpConnect struct {
	baseProxy
}

// address returns the address where the HTTP CONNECT proxy is exposed, i.e. proxy.host:proxy.port
func (p httpConnect) address() string {
	return fmt.Sprintf("%s:%s", p.host, p.port)
}

// handshake takes net.Conn (representing a TCP socket) and an address and returns the same net.Conn connected to the provided address through the HTTP CONNECT proxy
func (p httpConnect) handshake(conn net.Conn, address string) (err error) {

	gMetaLogger.Debugf("Entering CONNECT handshake(%v, %v)", conn, address)
	defer func() { gMetaLogger.Debugf("Exiting CONNECT handshake(%v, %v)", conn, address) }()

	if conn == nil {
		err = fmt.Errorf("nil conn was provided")
		return
	}

	reader := bufio.NewReader(conn)

	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return
	}

	var buff []byte
	if p.user != "" {
		gMetaLogger.Debugf("user is not empty, adding Proxy-Authorization header")
		auth := base64.StdEncoding.EncodeToString([]byte(p.user + ":" + p.pass))
		buff = []byte("CONNECT " + address + " HTTP/1.1\nHost: " + host + "\nProxy-Authorization: Basic " + auth + "\n\n")
	} else {
		buff = []byte("CONNECT " + address + " HTTP/1.1\nHost: " + host + "\n\n")
	}

	_, err = conn.Write(buff)
	if err != nil {
		return
	}
	gMetaLogger.Debugf("Wrote '%v' to the connection ", buff)
	gMetaLogger.Debugf("Wrote '%v' to the connection ", string(buff))

	response_line, err := reader.ReadString('\n')
	if err != nil {
		return
	}

	gMetaLogger.Debugf("proxy answer: %v", response_line)
	if !(strings.HasPrefix(response_line, "HTTP/1.0 2") || strings.HasPrefix(response_line, "HTTP/1.1 2") || strings.HasPrefix(response_line, "HTTP/2 2")) {
		err = fmt.Errorf("the proxy did not accept the connection and returned '%v'", response_line)
		return
	}

	gMetaLogger.Debug("Connection accepted, reading headers")

	for response_line != string([]byte{10}) && response_line != string([]byte{13, 10}) {
		gMetaLogger.Debug("reading new header line")
		response_line, err = reader.ReadString('\n')
		if err != nil {
			return
		}
		gMetaLogger.Debugf("Header line:\n%v%v", response_line, []byte(response_line))
	}

	return
}
