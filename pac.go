//go:build pac

package main

import (
	"fmt"
	"sync"

	"github.com/darren/gpac"
)

type pacConf struct {
	pac *gpac.Parser
	mu  sync.RWMutex
}

var gPACConf pacConf
var gPACcompiled bool = true

func reloadPACConf(path string) error {
	pac, err := gpac.FromFile(gArgPACPath)
	if err != nil {
		err = fmt.Errorf("error parsing PAC configuration: %v", err)
		return err
	}

	gPACConf.mu.Lock()
	gPACConf.pac = pac
	gPACConf.mu.Unlock()

	return nil
}

func getRouteWithPAC(addr string) (string, error) {
	gPACConf.mu.RLock()
	chainStr, err := gPACConf.pac.FindProxyForURL("rand://" + addr)
	gPACConf.mu.RUnlock()

	if err != nil {
		return "", err
	}

	return chainStr, nil
}
