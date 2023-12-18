package main

// This file contains the SOCKS5 implementation of the proxy interface defined in proxy.go

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
)

type socks5 struct {
	baseProxy
}

// address returns the address where the SOCKS5 proxy is exposed, i.e. proxy.host:proxy.port
func (p socks5) address() string {
	return fmt.Sprintf("%s:%s", p.host, p.port)
}

// handshake takes net.Conn (representing a TCP socket) and an address and returns the same net.Conn connected to the provided address through the SOCKS5 proxy
func (p socks5) handshake(conn net.Conn, address string) (err error) {
	gMetaLogger.Debugf("Entering SOCKS5 handshake(%v, %v)", conn, address)
	defer func() { gMetaLogger.Debugf("Exiting SOCKS5 handshake(%v, %v)", conn, address) }()

	//Implements SOCKS5 proxy handshake
	if conn == nil {
		err = fmt.Errorf("nil conn was provided")
		return
	}

	reader := bufio.NewReader(conn)

	if p.user != "" {
		//Means that user/password authentication method (0x02) is supported
		_, err = conn.Write([]byte{5, 2, 0, 2})
	} else {
		//Means only "no authentication" is supported
		_, err = conn.Write([]byte{5, 1, 0})
	}

	if err != nil {
		return
	}

	//Read server response containing  |VER|METHOD|
	buff := make([]byte, 2)
	_, err = io.ReadFull(reader, buff)
	if err != nil {
		return
	}

	ver := buff[0]
	method := buff[1]

	if ver != byte(5) {
		err = fmt.Errorf("SOCKS5 server's version != 5")
		return
	}

	switch method {
	case 0:

	case 2:
		err = fmt.Errorf("user/password method not yet implemented")
		return
	default:
		err = fmt.Errorf("unsupported authentication mechanism")
		return
	}

	buff = make([]byte, 4, 260)
	buff[0] = byte(5)
	buff[1] = byte(1)
	buff[2] = byte(0)

	addrBytes, atyp, err := stringToAddr(address)
	if err != nil {
		return
	}

	buff[3] = atyp
	buff = append(buff, addrBytes...)

	_, err = conn.Write(buff)
	if err != nil {
		return
	}
	gMetaLogger.Debugf("sent the following SOCKS request: %v", buff)

	buff = make([]byte, 4)
	_, err = io.ReadFull(reader, buff)
	if err != nil {
		err = fmt.Errorf("error reading SOCKS response: %w", err)
		return
	}
	gMetaLogger.Debugf("received the following SOCKS response: %v", buff)

	if rep := buff[1]; rep != byte(0) {
		switch rep {
		case 0x01:
			err = fmt.Errorf("general SOCKS server failure")
		case 0x02:
			err = fmt.Errorf("connection not allowed by ruleset")
		case 0x03:
			err = fmt.Errorf("network unreachable")
		case 0x04:
			err = fmt.Errorf("host unreachable")
		case 0x05:
			err = fmt.Errorf("connection refused")
		case 0x06:
			err = fmt.Errorf("TTL expired")
		case 0x07:
			err = fmt.Errorf("command not supported")
		case 0x08:
			err = fmt.Errorf("address type not supported")
		default:
			err = fmt.Errorf("custom SOCKS5 error byte %v", rep)
		}
		return
	}

	err = nil
	return
}

// stringToAddr takes a address string (format host:port) and returns the SOCKS5 defined address type atyp and the address bytes data in the SOCKS5 format (see RFC 1928)
func stringToAddr(addr string) (data []byte, atyp byte, err error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return
	}

	hostBytes := net.ParseIP(host)

	if hostBytes == nil { // host is a domain name
		atyp = 3
		hostBytes = []byte(host)
		hostBytes = append([]byte{byte(len(hostBytes))}, hostBytes...)
	} else { // host is an IP address
		hostBytesV4 := hostBytes.To4()
		if hostBytesV4 != nil { // host is an IPv4 address
			atyp = 1
			hostBytes = hostBytesV4
		} else { // host is an IPv6 address
			atyp = 4
		}

	}

	portInt, err := strconv.Atoi(port)
	if err != nil {
		return
	}

	portBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(portBytes, uint16(portInt))
	data = append(hostBytes, portBytes...)
	return
}

// addrToString takes a reader pointing to a SOCKS5 address formatted buffer and a SOCKS5 address type atyp (see RFC 1928) and returns an address string addr (format host:port)
func addrToString(reader io.Reader, atyp byte) (addr string, err error) {
	var buf []byte

	switch atyp {
	case atypIPV4:
		buf = make([]byte, 4)
	case atypIPV6:
		buf = make([]byte, 16)
	case atypDomain:
		size := make([]byte, 1)
		_, err = reader.Read(size)
		if err != nil {
			return
		}
		buf = make([]byte, int(size[0]))
	default:
		err = errors.New("invalid atyp value")
		return
	}

	// Read destination address
	_, err = io.ReadFull(reader, buf)
	if err != nil {
		return
	}

	var host string
	if atyp != atypDomain {
		host = net.IP(buf).String()
	} else {
		host = string(buf)
	}

	// Read destination port
	buf = make([]byte, 2)
	_, err = io.ReadFull(reader, buf)
	if err != nil {
		return
	}

	port := int(binary.BigEndian.Uint16(buf))
	addr = net.JoinHostPort(host, strconv.Itoa(port))
	return
}
