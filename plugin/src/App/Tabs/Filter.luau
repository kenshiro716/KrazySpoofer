--!strict
local UiLibrary = require("../../UiLibrary")

local MarketplaceService = game:GetService("MarketplaceService")
local Players = game:GetService("Players")
local GroupService = game:GetService("GroupService")


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

    local function createShareInput(parent: any, settingName: string, placeholder: string, tip: string, getName: (id: number) -> string)
        local function getPlaceholder(label: string?): string
            return if label then `Sharing with {label}` else placeholder
        end

        local savedId = plugin:GetSetting(settingName)
        local input; input = parent:CreateInput(getPlaceholder(if savedId and savedId ~= 0 then tostring(savedId) else nil), function(text)
            local id = tonumber(text)
            if not id then return end

            local textBox = input.instance.TextBox
            if id == 0 then
                plugin:SetSetting(settingName, 0)
                textBox.PlaceholderText = getPlaceholder()
                ui:Notify("Notification", "Sharing turned off.")
                return
            end

            local success, name = pcall(getName, id)
            if not success then
                ui:Notify("Notification", "Couldn't find that id.")
                return
            end

            plugin:SetSetting(settingName, id)
            textBox.PlaceholderText = getPlaceholder(name)
            ui:Notify("Notification", `Reuploads will be shared with {name}.`)
        end)
        input:AddTip(ui:CreateTip(tip))

        local textBox = input.instance.TextBox
        textBox:GetPropertyChangedSignal("Text"):Connect(function()
            textBox.Text = string.gsub(textBox.Text, "%D", "")
        end)
    end

    createShareInput(split2, "ShareUserId", "Share with user id",
        "Shares reuploaded animations, sounds and meshes with this user. They must be your friend. Enter 0 to turn it off.",
        function(id) return Players:GetNameFromUserIdAsync(id) end)

    local split3 = tab:CreateSplit()
    createShareInput(split3, "ShareGroupId", "Share with group id",
        "Shares reuploaded animations, sounds and meshes with this group. Enter 0 to turn it off.",
        function(id) return GroupService:GetGroupInfoAsync(id).Name end)

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
