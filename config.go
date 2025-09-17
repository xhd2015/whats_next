package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/xhd2015/less-gen/flags"
	"github.com/xhd2015/xgo/support/cmd"
)

// Mode represents the operation mode
type Mode string

const (
	ModeNative Mode = "native"
	ModeServer Mode = "server"
)

// Config represents the configuration stored in config.json
type Config struct {
	Editor          string `json:"editor"`
	SelectedProfile string `json:"selectedProfile"`
	Mode            Mode   `json:"mode"`
}

const configHelp = `
Usage:
  whats_next config --editor=editor

Options:
  --editor=editor  The editor to use for editing the config
`

func handleConfig(args []string) error {
	var editor string
	args, err := flags.String("--editor", &editor).Help("-h,--help", configHelp).Parse(args)
	if err != nil {
		return err
	}
	if len(args) > 0 {
		return fmt.Errorf("unrecognized extra arguments: %s", strings.Join(args, " "))
	}

	configPath, err := getConfigPath(false, "config.json")
	if err != nil {
		return err
	}

	editor = getEditor(editor)
	return cmd.Run(editor, configPath)
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
