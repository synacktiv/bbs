package main

// Defines a function to parse the JSON proxies and chains configuration file and a structure to store the parsed configuration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

type chainsConf struct {
	proxychains map[string]proxyChain
	valid       bool // whether the current configuration is valid
	mu          sync.RWMutex
}

type mainConfig struct {
	Proxies proxyMap
	Chains  chainMap
	Routes  routing
	Servers []server
	Hosts   hostMap
}

func parseMainConfig(configPath string) (mainConfig, error) {

	var config mainConfig

	fileBytes, err := os.ReadFile(configPath)
	if err != nil {
		err := fmt.Errorf("error reading file %v : %v", configPath, err)
		return config, err
	}

	dec := json.NewDecoder(bytes.NewReader(fileBytes))
	dec.DisallowUnknownFields()

	err = dec.Decode(&config)
	if err != nil {
		err = fmt.Errorf("error unmarshalling server config file : %v", err)
		return config, err
	}

	return config, nil

}
