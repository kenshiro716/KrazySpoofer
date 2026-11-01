--!strict
local License = require("../License")
local UiLibrary = require("../../UiLibrary")

return function(ui: UiLibrary.Ui, plugin: Plugin)
    local tab = ui:CreateTab("Settings")

    local split = tab:CreateSplit()
    local portInput; portInput = split:CreateInput("Port: " .. plugin:GetSetting("Port"), function(input)
        local newPort = tonumber(input)
        if not newPort then return end

        if newPort < 1024 or newPort > 49151 then
            ui:Notify("Notification", "Port has to be between 1024-49151.")
            return
        end

        plugin:SetSetting("Port", newPort)
        portInput.instance.TextBox.PlaceholderText = "Port: " .. newPort
    end)
    split:CreateToggle("Export json when done", function(state)
        plugin:SetSetting("ExportJSON", state)
    end, plugin:GetSetting("ExportJSON"))

    local licenseSplit = tab:CreateSplit()
    local licenseInput; licenseInput = licenseSplit:CreateInput(plugin:GetSetting("LicenseKey") and "License key saved" or "License key", function(input)
        local licensed, message = License.check(input)
        ui:Notify("Notification", message)
        if not licensed then return end

        plugin:SetSetting("LicenseKey", input)
        licenseInput.instance.TextBox.PlaceholderText = "License key saved"
    end)
end
