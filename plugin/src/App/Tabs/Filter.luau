--!strict
local UiLibrary = require("../../UiLibrary")

local MarketplaceService = game:GetService("MarketplaceService")
local Players = game:GetService("Players")

local function sharePlaceholder(label: string?): string
    return if label then `Sharing with {label}` else "Share with user id"
end

return function(ui: UiLibrary.Ui, plugin: Plugin)
	local tab = ui:CreateTab("Filter")
        local placeList

    _G.getPlaceList = function(): { number } -- guys im so sorry im just so drained :sob: will be back at full on v2.0.0, forgive me for _G use... I wont forgive myself
        local placesListValues = placeList:Get()
        local list = table.create(#placesListValues)
        for _, value in placesListValues do
            table.insert(list, tonumber(value))
        end
        return list :: { number }
    end
	
	local split1 = tab:CreateSplit()
    split1:CreateToggle("Search string values", function(state)
        plugin:SetSetting("StrVals", state)
    end, plugin:GetSetting("StrVals"))
    split1:CreateToggle("Search scripts", function(state)
        plugin:SetSetting("Scripts", state)
    end, plugin:GetSetting("Scripts"))
    
    local split2 = tab:CreateSplit()
    split2:CreateToggle("Search int/number values", function(state)
        plugin:SetSetting("NumVals", state)
    end, plugin:GetSetting("NumVals"))

    local savedShareUserId = plugin:GetSetting("ShareUserId")
    local shareInput; shareInput = split2:CreateInput(sharePlaceholder(if savedShareUserId and savedShareUserId ~= 0 then tostring(savedShareUserId) else nil), function(input)
        local userId = tonumber(input)
        if not userId then return end

        local shareTextBox = shareInput.instance.TextBox
        if userId == 0 then
            plugin:SetSetting("ShareUserId", 0)
            shareTextBox.PlaceholderText = sharePlaceholder()
            ui:Notify("Notification", "Sharing turned off.")
            return
        end

        local success, username = pcall(Players.GetNameFromUserIdAsync, Players, userId)
        if not success then
            ui:Notify("Notification", "Couldn't find a user with that id.")
            return
        end

        plugin:SetSetting("ShareUserId", userId)
        shareTextBox.PlaceholderText = sharePlaceholder(username)
        ui:Notify("Notification", `Reuploads will be shared with {username}. They must be your friend.`)
    end)
    shareInput:AddTip(ui:CreateTip(
        "Shares reuploaded animations, sounds and meshes with this user. They must be your friend. Enter 0 to turn it off."
    ))

    local shareTextBox = shareInput.instance.TextBox
    shareTextBox:GetPropertyChangedSignal("Text"):Connect(function()
        shareTextBox.Text = string.gsub(shareTextBox.Text, "%D", "")
    end)

    placeList = tab:CreateList()

	local placeInput = placeList:AddInput("Place Id", function(input)
		local placeId = tonumber(input)
        local success, productInfo = pcall(MarketplaceService.GetProductInfo, MarketplaceService, placeId)

        if not success then
            ui:Notify("Notification", "Error getting product info.") 
            return 
        end

        local assetTypeId = (productInfo :: any).AssetTypeId
		if assetTypeId ~= 9 then
            ui:Notify("Notification", "Invalid place id.") 
            return 
        end

        local stringPlaceId = tostring(placeId)
		if placeList.elements[stringPlaceId] then
            ui:Notify("Notification", "Place id is in list.") 
            return 
        end

		local element = placeList:CreateElement(stringPlaceId)
            :AddDestroyButton()
        
        element.instance.TextLabel.Text = `({placeId}) {(productInfo :: any).Name}`
	end)
	placeInput:AddTip(ui:CreateTip(
		"Reuploading needs a place id. Public games under the creator are found automatically."
	))

    local textBox = placeInput.instance.TextBox
    textBox:GetPropertyChangedSignal("Text"):Connect(function() 
        textBox.Text = string.gsub(textBox.Text, "%D", "") 
    end)
end
