--!strict
local App = {}

local UiLibrary = require("./UiLibrary")
local VersionGate = require(script.VersionGate)

local tabOrder = {
    "Animation",
    "Sound",
    "Mesh",
    "Products",
    "Replace",
    "Filter",
    "Themes",
    "Settings"
}

function App.init(ui: UiLibrary.Ui, plugin: Plugin)
    for _, tabName in tabOrder do
        local tab = script.Tabs:FindFirstChild(tabName)
        assert(tab, "tab not found")
        task.spawn(require(tab), ui, plugin)
    end

    local upToDate, message = VersionGate.check()
    if not upToDate then
        ui:Notify("Notification", message)
    end
end

return App