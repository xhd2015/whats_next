package main

import (
	"os"
	"path/filepath"
	"strings"
)


func groupShow(use bool, args []string) error {
	groupDir, err := getConfigPath(false, "group")
	if err != nil {
		return err
	}
	name, err := selectGroupName(groupDir, args)
	if err != nil {
		return err
	}
	name = addMDSuffix(name)

	groupFile := filepath.Join(groupDir, name)
	group, readErr := os.ReadFile(groupFile)
	if readErr != nil {
		return readErr
	}

	// Filter content based on project paths if using the profile
	if use {
		filteredContent, err := filterContentByProject(string(group))
		if err != nil {
			return err
		}
		printlnContent(os.Stdout, replaceWhatsNextWithProgramName(filteredContent))
	} else {
		printlnContent(os.Stdout, string(group))
	}

	if use {
		// Save selected profile to config
		config, err := readConfig()
		if err != nil {
			return err
		}
		config.SelectedProfile = strings.TrimSuffix(name, ".md")
		if err := writeConfig(config); err != nil {
			return err
		}

		return nil
	}
	return nil
}
