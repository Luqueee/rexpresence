local installedExecutable = nil
local function executablePath()
  if installedExecutable then return installedExecutable end
  if os.getenv("OS") == "Windows_NT" then
    local base = os.getenv("LOCALAPPDATA")
    if not base or base == "" then error("LOCALAPPDATA is not set; cannot find rexpresence.", 0) end
    return base .. "\\Programs\\rexpresence\\rexpresence.exe"
  end
  local home = os.getenv("HOME")
  if not home or home == "" then error("HOME is not set; cannot find rexpresence.", 0) end
  return home .. "/.local/bin/rexpresence"
end

rex.action{
  name = "discord_change_presence",
  title = "Discord: Change Presence…",
  args = { mode = "string?", text = "string?" },
  run = function(_, args)
    local mode = args.mode
    local text = args.text
    local fallback = "Use rex do discord_change_presence mode=auto, mode=paused, mode=pause, mode=resume, or mode=custom text=TEXT."
    local executable = executablePath()

    if mode == nil then
      if not rex.ui or not rex.ui.pick then
        error("This Rex version does not support interactive prompts. " .. fallback, 0)
      end
      local title = "Discord presence"
      local items = {
        { id = "auto", title = "Automatic" },
        { id = "custom", title = "Custom public text" },
        { id = "pause", title = "Pause for 15 minutes", subtitle = "Then return to your current presence" },
        { id = "paused", title = "Pause indefinitely" },
      }
      local result = rex.exec({ executable, "mode", "remaining" })
      local minutes = result and result.ok and tonumber(result.stdout)
      if minutes and minutes > 0 then
        title = "Paused · " .. minutes .. " min left"
        table.insert(items, 1, { id = "resume", title = "Resume now", subtitle = "Restore your previous presence" })
      end
      local why
      mode, why = rex.ui.pick{ title = title, items = items }
      if mode == nil then
        if why == "cancelled" then return end
        error("Could not choose Discord presence: " .. (why or "unknown error") .. ". " .. fallback, 0)
      end
    end

    if mode ~= "auto" and mode ~= "custom" and mode ~= "paused" and mode ~= "pause" and mode ~= "resume" then
      error("Invalid presence mode. " .. fallback, 0)
    end
    if mode == "custom" and text == nil then
      if not rex.ui or not rex.ui.input then
        error("This Rex version does not support interactive prompts. " .. fallback, 0)
      end
      local why
      text, why = rex.ui.input{ title = "Public Discord presence text" }
      if text == nil then
        if why == "cancelled" then return end
        error("Could not enter Discord presence text: " .. (why or "unknown error") .. ". " .. fallback, 0)
      end
    end
    if mode ~= "custom" and text ~= nil then
      error("Text is only valid with custom mode.", 0)
    end

    local argv = { executable, "mode", mode }
    if mode == "pause" or mode == "resume" then argv = { executable, mode } end
    if mode == "custom" then argv[4] = text end
    local result = rex.exec(argv)
    if result == nil then error("Could not start rexpresence; check the binary is installed.", 0) end
    if not result.ok and mode == "pause" and (result.stderr or ""):find("already paused indefinitely", 1, true) then
      error("Already paused indefinitely. Choose Automatic or Custom public text to resume.", 0)
    end
    if not result.ok and mode == "resume" and (result.stderr or ""):find("no active timed pause", 1, true) then
      error("The timed pause is no longer active. Your presence was not changed.", 0)
    end
    if not result.ok then error("rexpresence mode failed (exit " .. tostring(result.code) .. ").", 0) end
    return { mode = mode }
  end,
}

rex.action{
  name = "discord_configure_card",
  title = "Discord: Configure Card…",
  args = { field = "string?", value = "string?", reset = "boolean?" },
  run = function(_, args)
    local field, value, reset = args.field, args.value, args.reset
    local fallback = "Use rex do discord_configure_card field=line1|line2|logo|timer value=VALUE, or reset=true."
    if reset and value ~= nil then error("Cannot set and reset a card field together.", 0) end
    if field == nil then
      if value ~= nil or reset then error("Choose a card field. " .. fallback, 0) end
      if not rex.ui or not rex.ui.pick then
        error("This Rex version does not support interactive prompts. " .. fallback, 0)
      end
      local choice, why = rex.ui.pick{
        title = "Discord card",
        items = {
          { id = "line1", title = "Top line" },
          { id = "line2", title = "Bottom line" },
          { id = "logo", title = "Logo" },
          { id = "timer", title = "Elapsed time" },
        },
      }
      if choice == nil then
        if why == "cancelled" then return end
        error("Could not choose a card field. " .. fallback, 0)
      end
      field = choice
    end
    if field ~= "line1" and field ~= "line2" and field ~= "logo" and field ~= "timer" then
      error("Invalid card field. " .. fallback, 0)
    end
    local executable = executablePath()
    local function run(argv)
      local result = rex.exec(argv)
      if result == nil then error("Could not start rexpresence; check the binary is installed.", 0) end
      if not result.ok then error("rexpresence card command failed (exit " .. tostring(result.code) .. ").", 0) end
      return result
    end
    if value == nil and not reset then
      if not rex.ui or not rex.ui.pick then
        error("This Rex version does not support interactive prompts. " .. fallback, 0)
      end
      local current = run({ executable, "card", "get", field }).stdout
      if type(current) ~= "string" then error("Could not read the current card field.", 0) end
      current = current:gsub("\n$", "")
      local title, items
      if field == "timer" then
        title = "Elapsed time"
        items = { { id = "on", title = "Show timer" }, { id = "off", title = "Hide timer" } }
      elseif field == "logo" then
        title = "Discord logo"
        items = {
          { id = "edit", title = "Use my own image…" },
          { id = "hide", title = "Hide logo" },
        }
      else
        title = field == "line1" and "What goes on the top line?" or "What goes on the bottom line?"
        items = {
          { id = "{group}", title = "Rex group name" },
          { id = "{tool}", title = "Current tool" },
          { id = "{project}", title = "Project alias", subtitle = "Hidden if no public project alias is set" },
          { id = "{activity}", title = "Tool and project" },
          { id = "edit", title = "Custom text or template…" },
          { id = "hide", title = "Hide this line" },
        }
      end
      items[#items + 1] = { id = "reset", title = "Restore default" }
      local known = false
      for _, item in ipairs(items) do
        local active = item.id == current and item.id ~= "edit" and item.id ~= "reset" and item.id ~= "hide"
          or item.id == "hide" and current == ""
          or item.id == "reset" and (field == "logo" and current == "{default}" or field == "line2" and current == "Group: {group}")
        if active then
          item.title = item.title .. " (current)"
          known = true
        end
      end
      while true do
        local choice, why = rex.ui.pick{ title = title, items = items }
        reset, value = false, nil
        if choice == "reset" then
          reset = true
        elseif choice == "hide" then
          value = ""
        elseif choice == "edit" then
          if not rex.ui.input then error("Text input is unavailable. " .. fallback, 0) end
          local prompt = field == "logo" and "Image URL or asset key" or "Custom text or template"
          value, why = rex.ui.input{ title = prompt, value = field == "logo" and known and "" or current, confirm = "Preview change" }
        else
          for _, item in ipairs(items) do
            if item.id == choice then value = choice; break end
          end
        end
        if value == nil and not reset then
          if why == "cancelled" then return end
          error("Could not configure the card field. " .. fallback, 0)
        end
        local argv = { executable, "card", "preview", field }
        if not reset then argv[5] = value end
        local preview = rex.exec(argv)
        if not preview or not preview.ok or type(preview.stdout) ~= "string" then
          error("Card preview is unavailable; no changes were made.", 0)
        end
        local rows = {}
        for row in (preview.stdout .. "\n"):gmatch("([^\n]*)\n") do rows[#rows + 1] = row end
        if rows[#rows] == "" then rows[#rows] = nil end
        if #rows ~= 5 then error("Card preview is unavailable; no changes were made.", 0) end
        local fieldRow = ({ line1 = 2, line2 = 3, logo = 4, timer = 5 })[field]
        local confirm, reason = rex.ui.pick{
          title = "Save this change?",
          items = {
            { id = "apply", title = "Apply change", subtitle = rows[fieldRow], detail = rows[1] },
            { id = "back", title = "Back", subtitle = "Choose another option without saving" },
          },
        }
        if confirm == "apply" then break end
        if confirm == nil and reason == "cancelled" then return end
        if confirm ~= "back" then error("Could not confirm the change; nothing was saved.", 0) end
      end
    end
    if reset then
      run({ executable, "card", "reset", field })
      return { field = field, reset = true }
    end
    if field == "timer" and value ~= "on" and value ~= "off" then error("Timer must be on or off.", 0) end
    run({ executable, "card", "set", field, value })
    return { field = field }
  end,
}
