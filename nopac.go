//go:build !pac

package main

import (
	"fmt"
)

var gPACcompiled bool = false

func reloadPACConf(path string) error {
	err := fmt.Errorf("bbs compiled without PAC support")
	return err
}

func getRouteWithPAC(addr string) (string, error) {
	err := fmt.Errorf("bbs compiled without PAC support")
	return "", err
}
