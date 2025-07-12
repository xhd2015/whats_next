package main

import (
	"encoding/json"
	"os"
)

// Config represents the configuration stored in config.json
type Config struct {
	SelectedProfile string `json:"selectedProfile"`
}

// readConfig reads the config from config.json
func readConfig() (*Config, error) {
	configFile, err := getConfigPath(false, "config.json")
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, err
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

// writeConfig writes the config to config.json
func writeConfig(config *Config) error {
	configFile, err := getConfigPath(true, "config.json")
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(configFile, data, 0644)
}
