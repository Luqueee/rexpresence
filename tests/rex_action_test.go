package integration_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRexPresenceAction(t *testing.T) {
	lua, err := exec.LookPath("lua")
	if err != nil {
		t.Skip("Lua interpreter not installed")
	}
	script := `
local actions, action, calls, picked, input, reason, result = {}
local remaining = "0\n"
rex = {
  action = function(a) actions[a.name] = a end,
  ui = {
    pick = function(_) return picked, reason end,
    input = function(_) return input, reason end,
  },
  exec = setmetatable({}, { __call = function(_, argv)
    if argv[3] == "remaining" then return { ok = true, stdout = remaining } end
    calls = calls + 1
    assert(argv[1] == os.getenv("HOME") .. "/.local/bin/rexpresence")
    assert(argv[2] == "mode")
    assert(argv[3] == picked)
    if picked == "custom" then assert(argv[4] == input) else assert(argv[4] == nil) end
    return result
  end }),
}
dofile(arg[1])
action = actions.discord_change_presence
assert(action.name == "discord_change_presence")
assert(action.title == "Discord: Change Presence…")
result, calls = { ok = true, code = 0 }, 0
picked = "auto"
assert(action.run({}, {}).mode == "auto" and calls == 1)
picked, input = "custom", "public text; $(not-a-shell)"
assert(action.run({}, {}).mode == "custom" and calls == 2)
picked = "paused"
assert(action.run({}, { mode = "paused" }).mode == "paused" and calls == 3)
picked = nil
reason = "cancelled"
assert(action.run({}, {}) == nil and calls == 3)
reason = "unsupported"
local ok, err = pcall(action.run, {}, {})
assert(not ok and string.find(err, "unsupported", 1, true) and calls == 3)
rex.ui = {
  pick = function(_) return "custom" end,
  input = function(_) return nil, "cancelled" end,
}
assert(action.run({}, {}) == nil and calls == 3)
ok, err = pcall(action.run, {}, { mode = "unknown" })
assert(not ok and calls == 3)
rex.ui = nil
ok, err = pcall(action.run, {}, {})
assert(not ok and string.find(err, "mode=auto", 1, true) and calls == 3)
picked, input = "custom", "secret"
result = { ok = false, code = 1, stderr = "secret" }
ok, err = pcall(action.run, {}, { mode = "custom", text = input })
assert(not ok and not string.find(err, "secret", 1, true) and calls == 4)
result, picked = { ok = true, code = 0 }, "pause-15m"
rex.ui = { pick = function(prompt)
  assert(prompt.title == "Discord presence")
  for _, item in ipairs(prompt.items) do assert(item.id ~= "resume") end
  for _, item in ipairs(prompt.items) do if item.id == picked then return picked end end
end }
assert(action.run({}, {}).mode == "pause-15m" and calls == 5)
remaining, picked = "8\n", "resume"
rex.ui.pick = function(prompt)
  assert(prompt.title == "Paused · 8 min left" and prompt.items[1].id == "resume")
  return prompt.items[1].id
end
assert(action.run({}, {}).mode == "resume" and calls == 6)

local card = actions.discord_configure_card
assert(card and card.title == "Discord: Configure Card…")
local commands, answers, prompts = {}, {}, {}
local current = "  current value  \n"
local preview = "Mode: auto\nTop line: resolved group\nBottom line: resolved project\nLogo: default\nTimer: running\n"
rex.exec = function(argv)
  assert(argv[1] == os.getenv("HOME") .. "/.local/bin/rexpresence")
  assert(argv[2] == "card")
  commands[#commands + 1] = argv
  return { ok = true, code = 0, stdout = argv[3] == "preview" and preview or current }
end
rex.ui = {
  pick = function(prompt)
    prompts[#prompts + 1] = prompt
    local answer = table.remove(answers, 1)
    if answer == false then return nil, "cancelled" end
    if answer == "unexpected" then return answer end
    for _, item in ipairs(prompt.items) do
      if item.id == answer then return answer end
    end
    error("Missing picker choice: " .. tostring(answer))
  end,
  input = function(prompt)
    prompts[#prompts + 1] = prompt
    local answer = table.remove(answers, 1)
    if answer == false then return nil, "cancelled" end
    return answer
  end,
}
local function choose(...)
  commands, prompts, answers = {}, {}, {...}
  return card.run({}, {})
end
local function saved(field, value)
  local last = commands[#commands]
  assert(last[3] == "set" and last[4] == field and last[5] == value)
end
choose("line2", "{group}", "apply")
assert(commands[2][3] == "preview" and commands[2][4] == "line2" and commands[2][5] == "{group}")
saved("line2", "{group}")
assert(#prompts == 3 and #prompts[3].items == 2)
assert(prompts[3].items[1].id == "apply" and prompts[3].items[1].subtitle == "Bottom line: resolved project")
assert(prompts[3].items[1].detail == "Mode: auto" and prompts[3].items[2].id == "back")
choose("line1", "edit", "  new; $(literal)  ", "apply")
assert(prompts[3].value == "  current value  ")
assert(commands[2][5] == "  new; $(literal)  ")
saved("line1", "  new; $(literal)  ")
choose("line2", "hide", "apply")
assert(commands[2][3] == "preview" and commands[2][5] == "")
saved("line2", "")
choose("logo", "hide", "apply")
saved("logo", "")
assert(prompts[3].items[1].subtitle == "Logo: default")
current = "{default}\n"
choose("logo", "edit", "team-logo", "apply")
assert(prompts[3].value == "")
saved("logo", "team-logo")
choose("timer", "off", "apply")
saved("timer", "off")
assert(prompts[3].items[1].subtitle == "Timer: running")
choose("line2", "reset", "apply")
assert(commands[2][3] == "preview" and commands[2][4] == "line2" and commands[2][5] == nil)
assert(commands[3][3] == "reset" and commands[3][4] == "line2")
choose("line2", "{group}", "back", "hide", "apply")
assert(commands[2][3] == "preview" and commands[2][5] == "{group}")
assert(commands[3][3] == "preview" and commands[3][5] == "")
saved("line2", "")
assert(prompts[4].title == prompts[2].title)
choose("line2", "reset", "back", false)
assert(#commands == 2 and commands[2][3] == "preview")
choose("line2", "hide", false)
assert(#commands == 2 and commands[2][3] == "preview")
commands, prompts, answers = {}, {}, { "line2", "hide", "unexpected" }
ok, err = pcall(card.run, {}, {})
assert(not ok and #commands == 2)
choose(false)
assert(#commands == 0)
choose("line2", false)
assert(#commands == 1 and commands[1][3] == "get")
choose("line2", "edit", false)
assert(#commands == 1 and commands[1][3] == "get")
commands = {}
card.run({}, { field = "logo", value = "" })
saved("logo", "")
card.run({}, { field = "timer", reset = true })
assert(commands[2][3] == "reset" and commands[2][4] == "timer")
ok, err = pcall(card.run, {}, { field = "timer", value = "secret" })
assert(not ok and not string.find(err, "secret", 1, true) and #commands == 2)
rex.exec = function(_) return { ok = false, code = 1, stderr = "secret" } end
ok, err = pcall(card.run, {}, { field = "line1", value = "secret" })
assert(not ok and not string.find(err, "secret", 1, true))
rex.exec = function(argv)
  commands[#commands + 1] = argv
  if argv[3] == "get" then return { ok = true, stdout = current } end
  return { ok = false, stderr = "secret" }
end
commands, prompts, answers = {}, {}, { "line1", "edit", "secret" }
ok, err = pcall(card.run, {}, {})
assert(not ok and not string.find(err, "secret", 1, true) and #commands == 2)
rex.exec = function(argv)
  commands[#commands + 1] = argv
  return { ok = true, stdout = argv[3] == "get" and current or "secret" }
end
commands, prompts, answers = {}, {}, { "line1", "hide" }
ok, err = pcall(card.run, {}, {})
assert(not ok and not string.find(err, "secret", 1, true) and #commands == 2)
`
	path, err := filepath.Abs(filepath.Join("..", "integrations", "rex", "rexpresence.lua"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(lua, "-", path)
	cmd.Stdin = strings.NewReader(script)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("action test failed: %v\n%s", err, output)
	}
}
