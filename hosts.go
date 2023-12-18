package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// loadHosts parses the JSON custom hosts resolution configuration file at configPath and returns a map IP addresses representing this configuration.
func loadHosts(path string) (hosts map[string]string, err error) {

	if path == "" {
		err = fmt.Errorf("'path' is empty")
		return
	}

	fileBytes, err := os.ReadFile(path)
	if err != nil {
		return
	}

	err = json.Unmarshal(fileBytes, &hosts)
	gMetaLogger.Debugf("Loaded custom hosts: %v", hosts)

	return
}
