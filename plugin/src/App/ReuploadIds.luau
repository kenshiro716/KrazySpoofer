--!strict
local AssetIdFilter = require("./AssetIdFilter")
local findDeveloperProducts = require("./FindDeveloperProducts")
local Connection = require("./Connection")
local retry = require("./Retry")
local StatusCodes = require("./StatusCodes")
local UiLibrary = require("../UiLibrary")
local VersionGate = require("./VersionGate")

local MarketplaceService = game:GetService("MarketplaceService")
local ScriptEditorService = game:GetService("ScriptEditorService")
local HttpService = game:GetService("HttpService")

local currentVersion = script.Parent.Parent.Parent.Version.Value
local busy = false

type ProductInfo = {
    AssetTypeId: number,
    TargetId: number,
    IsNew: boolean,
    Updated: string, -- ISO format
    AssetId: number,
    ProductId: number,
    MinimumMembershipLevel: number,
    Created: string, -- ISO format
    Creator: {
        Name: string,
        CreatorTargetId: number,
        Id: number,
        HasVerifiedBadge: boolean,
        CreatorType: string
    },
    IsLimited: boolean,
    ContentRatingTypeId: number,
    Sales: number,
    IsPublicDomain: boolean,
    Name: string,
    IsForSale: boolean,
    Description: string,
    IconImageAssetId: number,
    IsLimitedUnique: boolean
}

type Request = {
	placeId: number,         
	creatorId: number,       
	ids: { number },             
	defaultPlaceIds: { number }, 
	pluginVersion: string,   
	assetType: string,       
	exportJson: boolean,     
	isGroup: boolean,
	products: { findDeveloperProducts.ProductInfo }?,
	shareUserId: number?,
	shareGroupId: number?
}

local function scriptInjectionEnabled()
    local testScript = Instance.new("Script")
	local success = pcall(ScriptEditorService.UpdateSourceAsync, ScriptEditorService, testScript, function() 
		return "" 
	end)
	
	testScript:Destroy()
	return success
end

function getGameInfo(): (boolean, number, number)
    local creatorId, placeId = game.CreatorId, game.PlaceId

    if placeId == 0 then
        return false, creatorId, placeId 
    end

    if creatorId ~= 0 then
        local isGroup =  game.CreatorType == Enum.CreatorType.Group
        return isGroup, creatorId, placeId
    end

    local success, result = retry(3, MarketplaceService.GetProductInfo, MarketplaceService, placeId)
    if not success then
        return false, creatorId, placeId
    end

    local creatorInfo = (result :: ProductInfo).Creator
    local isGroup = creatorInfo.CreatorType == "Group"
    return isGroup, creatorInfo.CreatorTargetId, placeId
end

return function(plugin: Plugin, ui: UiLibrary.Ui, filterOptions: AssetIdFilter.FilterOptions, assetType: "Animation" | "Sound" | "Mesh" | "DeveloperProduct", placeArray: { number }?)
    if busy then
        ui:Notify("Notification", "Plugin is busy.")
        return 
    end
    busy = true

    local upToDate, versionMessage = VersionGate.check()
    if not upToDate then
        ui:Notify("Notification", versionMessage)
        busy = false
        return
    end

    if not scriptInjectionEnabled() then
        ui:Notify("Notification", "Plugin needs script injection permission.")
        busy = false
        return
    end

    local isGroup, creatorId, placeId = getGameInfo()
    if placeId == 0 then
        ui:Notify("Notification", "Game needs to be published. Restart place after publishing.")
        busy = false
        return
    elseif creatorId == 0 then
        ui:Notify("Notification", "Failed to get place info. Try again or restart the place.")
        busy = false
        return
    end
    
    local filteredIds, attributeIds = AssetIdFilter.filterInstances(filterOptions)
    if next(filteredIds) == nil and next(attributeIds) == nil then
        ui:Notify("Notification", "No ids found.")
        busy = false
        return
    end

    local products: { findDeveloperProducts.ProductInfo }?
    if assetType == "DeveloperProduct" then
        local candidates = AssetIdFilter.getIdArray(filteredIds)
        ui:Notify("Notification", `Checking {#candidates} ids for developer products...`)
        local found = findDeveloperProducts(candidates, function(checked, total)
            ui:Notify("Notification", `Checked {checked}/{total} ids for developer products...`)
        end)

        local productList = {}
        for id in filteredIds do
            local info = found[id]
            if info then
                table.insert(productList, info)
            else
                filteredIds[id] = nil
            end
        end

        if #productList == 0 then
            ui:Notify("Notification", "No developer products found.")
            busy = false
            return
        end
        products = productList
    end

    local connection, response = Connection.new(plugin:GetSetting("Port"))
    if not connection then
        ui:Notify("Notification", "Unable to connect to localhost. Did you start the executable?" )
        busy = false
        return
    end

    local function onDataRecieved(data: string)
        local success, result = pcall(HttpService.JSONDecode, HttpService, data)
        if not success then
            if data ~= "done" then return end
            
            ui:Notify("Notification", "Finished reuploading all ids.")
            busy = false
            connection:Destroy()
            return
        end

        if #(result :: any) == 0 then return end
        AssetIdFilter.replaceIds(filteredIds, result :: any, attributeIds)
        warn(`Replaced {#(result :: any)} ids`)
    end

    connection.onDataRecieved = onDataRecieved
    connection.onDisconnect = function()
        ui:Notify("Notification", "Disconnected from localhost. (Try reconnecting)")
        busy = false
    end

    local idArray = AssetIdFilter.getIdArray(filteredIds, attributeIds)
    local request: Request = {
        pluginVersion = currentVersion,
        creatorId = creatorId,
        isGroup = isGroup,
        assetType = assetType,
        ids = idArray,
        placeId = placeId,
        defaultPlaceIds = placeArray or {},
        exportJson = plugin:GetSetting("ExportJSON"),
        products = products,
        shareUserId = plugin:GetSetting("ShareUserId"),
        shareGroupId = plugin:GetSetting("ShareGroupId"),
    }
    
    local success, statusCode = connection:Send("/reupload", HttpService:JSONEncode(request))
    if not success then
        local message: string = (StatusCodes :: any)[statusCode] or ("Unable to send data to localhost.\n" .. statusCode)
        ui:Notify("Notification", message)

        busy = false
        connection:Destroy()
        return
    end

    ui:Notify("Notification", `Sent {#idArray} ids to localhost.`)

    if response then
        onDataRecieved(response)
    end
end
