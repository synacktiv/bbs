## gpac

[![PkgGoDev](https://pkg.go.dev/badge/github.com/darren/gpac)](https://pkg.go.dev/github.com/darren/gpac)

This package provides a pure Go [pac](https://developer.mozilla.org/en-US/docs/Web/HTTP/Proxy_servers_and_tunneling/Proxy_Auto-Configuration_(PAC)_file) parser based on [otto](https://github.com/robertkrimen/otto)

## Example usage

```go
package main

import (
	"fmt"

	"github.com/darren/gpac"
)

var scripts = `
  function FindProxyForURL(url, host) {
    if (isPlainHostName(host)) return DIRECT;
    else return "PROXY 127.0.0.1:8080; PROXY 127.0.0.1:8081; DIRECT";
  }
`

func main() {
	pac, _ := gpac.New(scripts)

	r, _ := pac.FindProxyForURL("http://www.example.com/")
	fmt.Println(r) // returns PROXY 127.0.0.1:8080; PROXY 127.0.0.1:8081; DIRECT

	// Get issues request via a list of proxies and returns at the first request that succeeds
	resp, _ := pac.Get("http://www.example.com/")
	fmt.Println(resp.Status)
}
```

## Simple wrapper for `curl` and `wget`

There's a simple tool that wraps `curl` and `wget` for pac file support.

### Install

```
go get  github.com/darren/gpac/gpacw
```

### Usage

```
gpacw wpad.dat curl -v http://example.com
gpacw http://wpad/wpad.dat wget -O /dev/null http://example.com
```

**note** url should be the last argument of the command or it will fail.
