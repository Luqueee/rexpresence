// Package palette embeds the Rex actions shipped with the CLI.
package palette

import (
	_ "embed"
	"strings"
)

//go:embed rexpresence.lua
var source string

// Module returns the actions with an executable path independent of Rex's PATH.
func Module(executable string) []byte {
	quoted := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\r", `\r`, "\n", `\n`).Replace(executable)
	return []byte(strings.Replace(source, "local installedExecutable = nil", `local installedExecutable = "`+quoted+`"`, 1))
}
