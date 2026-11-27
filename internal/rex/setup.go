package rex

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ConfigForSetup locates the local Rex CLI and its configuration without running either.
func ConfigForSetup(remove bool) (path string, validate func(string) error, reload func() error, err error) {
	c, err := findCLI()
	if err != nil {
		return "", nil, nil, fmt.Errorf("find Rex CLI: %w", err)
	}
	path = os.Getenv("REX_CONFIG")
	if path == "" {
		var dir string
		if runtime.GOOS == "windows" {
			dir = os.Getenv("APPDATA")
			if dir == "" {
				return "", nil, nil, fmt.Errorf("find Rex config: APPDATA is not set")
			}
		} else {
			dir = os.Getenv("XDG_CONFIG_HOME")
			if dir == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return "", nil, nil, fmt.Errorf("find Rex config home: %w", err)
				}
				dir = filepath.Join(home, ".config")
			}
		}
		path = filepath.Join(dir, "rex", "init.lua")
	}
	validate = func(file string) error {
		cmd := exec.Command(string(c), "--autostart=false", "config", "check", "--json", "--", file)
		output, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("Rex config check failed (%w); run rex config check on the file for details", err)
		}
		var result struct {
			Actions []struct {
				Name string `json:"name"`
			} `json:"actions"`
		}
		if err := json.Unmarshal(output, &result); err != nil {
			return fmt.Errorf("parse Rex config check JSON: %w; update Rex and run rex config check --json for details", err)
		}
		var presence, card bool
		for _, action := range result.Actions {
			switch action.Name {
			case "discord_change_presence":
				presence = true
			case "discord_configure_card":
				card = true
			}
		}
		if remove {
			if presence || card {
				return fmt.Errorf("the config still loads rexpresence actions; check for another reference to the palette")
			}
		} else if !presence || !card {
			return fmt.Errorf("Rex config did not load both rexpresence actions; ensure require(\"rexpresence\") runs unconditionally and the server uses this config (check its --config option)")
		}
		return nil
	}
	reload = func() error {
		cmd := exec.Command(string(c), "--autostart=false", "config", "reload")
		cmd.Env = []string{}
		for _, entry := range os.Environ() {
			key, _, _ := strings.Cut(entry, "=")
			if !strings.EqualFold(key, "REX_SERVER") && !strings.EqualFold(key, "REX_HOME") {
				cmd.Env = append(cmd.Env, entry)
			}
		}
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("reload local Rex config (%w); make sure the local Rex server is running and run rex --autostart=false config reload for details", err)
		}
		return nil
	}
	return path, validate, reload, nil
}
