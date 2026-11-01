--!strict
local AssetIdFilter = {}

local getAssetIds = require("./AssetIdFilter/GetAssetIds")
local changeIds = require("./AssetIdFilter/ChangeIds")

export type FilterOptions = getAssetIds.FilterOptions

function AssetIdFilter.filterInstances(filterOptions: getAssetIds.FilterOptions): ({ [number]: { Instance } }, { [number]: { Instance } })
    return getAssetIds(filterOptions)
end

function AssetIdFilter.getIdArray(filteredIds: { [number]: { Instance } }, attributeIds: { [number]: { Instance } }?): { number }
    local idArray = {}
    for id, _ in filteredIds do
        table.insert(idArray, id)
    end
    for id, _ in attributeIds or {} do
        if filteredIds[id] then continue end
        table.insert(idArray, id)
    end
    return idArray
end

function AssetIdFilter.replaceIds(filteredIds: { [number]: { Instance } }, idsToChange: { changeIds.IdPair }, attributeIds: { [number]: { Instance } }?)
    changeIds(filteredIds, idsToChange, attributeIds)
end

return AssetIdFilter
