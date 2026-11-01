--!strict
local VersionGate = {}

local HttpService = game:GetService("HttpService")

-- Separate from the Version value, which has to keep matching the executable.
-- To force an update: raise this, release the plugin, then raise "build" in
-- latest-plugin.json to the same number.
local BUILD = 1
local LATEST_URL = "https://raw.githubusercontent.com/kenshiro716/KrazySpoofer/main/latest-plugin.json"

function VersionGate.isOutdated(body: string): (boolean?, string)
    local decoded, latest = pcall(HttpService.JSONDecode, HttpService, body)
    if not decoded or type(latest) ~= "table" or type((latest :: any).build) ~= "number" then
        return nil, "Couldn't check for updates. Try again later."
    end

    if BUILD < (latest :: any).build then
        return true, "Need latest"
    end
    return false, ""
end

-- Returns false and a message when the plugin must not run.
function VersionGate.check(): (boolean, string)
    local fetched, body = pcall(HttpService.GetAsync, HttpService, LATEST_URL, true)
    if not fetched then
        return false, "Couldn't check for updates. Allow HTTP access for this plugin and try again."
    end

    local outdated, message = VersionGate.isOutdated(body)
    return outdated == false, message
end

return VersionGate
