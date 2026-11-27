package rex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// PaletteStatus counts the rexpresence actions offered by the local Rex server.
func PaletteStatus(ctx context.Context) (expectedActions int, err error) {
	c, err := findCLI()
	if err != nil {
		return 0, fmt.Errorf("find Rex CLI: %w; install Rex or add its CLI to PATH", err)
	}
	cmd := exec.CommandContext(ctx, string(c), "--autostart=false", "actions", "--json")
	cmd.Env = []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.EqualFold(key, "REX_SERVER") && !strings.EqualFold(key, "REX_CLIENT") && !strings.EqualFold(key, "REX_HOME") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("query local Rex server (%w); ensure Rex is running and its CLI can connect", err)
	}
	var result struct {
		Actions json.RawMessage `json:"actions"`
	}
	if json.Unmarshal(output, &result) != nil || len(result.Actions) == 0 {
		return 0, fmt.Errorf("invalid Rex actions JSON; update Rex and try again")
	}
	var actions []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(result.Actions, &actions) != nil {
		return 0, fmt.Errorf("invalid Rex actions JSON; update Rex and try again")
	}
	var presence, card bool
	for _, action := range actions {
		switch action.Name {
		case "discord_change_presence":
			presence = true
		case "discord_configure_card":
			card = true
		}
	}
	if presence {
		expectedActions++
	}
	if card {
		expectedActions++
	}
	return expectedActions, nil
}
