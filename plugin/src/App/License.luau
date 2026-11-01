--!strict
local License = {}

local HttpService = game:GetService("HttpService")
local StudioService = game:GetService("StudioService")

local LICENSE_URL = "https://raw.githubusercontent.com/kenshiro716/Asset-Reuploader/main/licenses.json"

local INITIAL_HASH = {
    0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a, 0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19,
}

local ROUND_CONSTANTS = {
    0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1, 0x923f82a4, 0xab1c5ed5,
    0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3, 0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174,
    0xe49b69c1, 0xefbe4786, 0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
    0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147, 0x06ca6351, 0x14292967,
    0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13, 0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85,
    0xa2bfe8a1, 0xa81a664b, 0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
    0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a, 0x5b9cca4f, 0x682e6ff3,
    0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208, 0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2,
}

local function sha256(message: string): string
    local hash = table.clone(INITIAL_HASH)
    local padding = string.rep("\0", (55 - #message) % 64)
    local padded = message .. "\128" .. padding .. string.pack(">I8", #message * 8)

    for offset = 1, #padded, 64 do
        local w = table.create(64, 0)
        for i = 1, 16 do
            w[i] = string.unpack(">I4", padded, offset + (i - 1) * 4)
        end
        for i = 17, 64 do
            local s0 = bit32.bxor(bit32.rrotate(w[i - 15], 7), bit32.rrotate(w[i - 15], 18), bit32.rshift(w[i - 15], 3))
            local s1 = bit32.bxor(bit32.rrotate(w[i - 2], 17), bit32.rrotate(w[i - 2], 19), bit32.rshift(w[i - 2], 10))
            w[i] = (w[i - 16] + s0 + w[i - 7] + s1) % 2 ^ 32
        end

        local a, b, c, d, e, f, g, h = table.unpack(hash)
        for i = 1, 64 do
            local s1 = bit32.bxor(bit32.rrotate(e, 6), bit32.rrotate(e, 11), bit32.rrotate(e, 25))
            local choice = bit32.bxor(bit32.band(e, f), bit32.band(bit32.bnot(e), g))
            local temp1 = (h + s1 + choice + ROUND_CONSTANTS[i] + w[i]) % 2 ^ 32
            local s0 = bit32.bxor(bit32.rrotate(a, 2), bit32.rrotate(a, 13), bit32.rrotate(a, 22))
            local majority = bit32.bxor(bit32.band(a, b), bit32.band(a, c), bit32.band(b, c))
            local temp2 = (s0 + majority) % 2 ^ 32

            h, g, f, e, d, c, b, a = g, f, e, (d + temp1) % 2 ^ 32, c, b, a, (temp1 + temp2) % 2 ^ 32
        end

        for i, value in { a, b, c, d, e, f, g, h } do
            hash[i] = (hash[i] + value) % 2 ^ 32
        end
    end

    local hex = table.create(8)
    for i, value in hash do
        hex[i] = string.format("%08x", value)
    end
    return table.concat(hex)
end

local function normalizeKey(key: string): string
    return string.upper((string.gsub(key, "%s", "")))
end

-- Keys are stored online only as sha256("KEY:robloxUserId"), so a key does
-- nothing on another account and can be revoked by removing its hash.
function License.check(key: string?): (boolean, string)
    if not key or key == "" then
        return false, "License key required. Enter it in the Settings tab."
    end

    local userId = StudioService:GetUserId()
    if userId == 0 then
        return false, "Log in to Studio to use your license key."
    end

    local fetched, body = pcall(HttpService.GetAsync, HttpService, LICENSE_URL, true)
    if not fetched then
        return false, "Couldn't check your license. Allow HTTP access for this plugin and try again."
    end

    local decoded, licenses = pcall(HttpService.JSONDecode, HttpService, body)
    if not decoded or type(licenses) ~= "table" or type((licenses :: any).hashes) ~= "table" then
        return false, "Couldn't read the license list. Try again later."
    end

    local keyHash = sha256(`{normalizeKey(key)}:{userId}`)
    if table.find((licenses :: any).hashes, keyHash) then
        return true, "License active."
    end
    return false, "Invalid license key for this Roblox account."
end

return License
