package request

import (
	"github.com/kenshiro716/KrazySpoofer/internal/roblox"
	"github.com/kenshiro716/KrazySpoofer/internal/roblox/games"
)

// ProductInfo describes a developer product read by the plugin with
// MarketplaceService:GetProductInfo, since its details are not public via the web API.
type ProductInfo struct {
	ID               int64  `json:"id"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	Price            int64  `json:"price"`
	IconImageAssetID int64  `json:"iconImageAssetId"`
}

type RawRequest struct {
	PlaceID         int64   `json:"placeId"`
	CreatorID       int64   `json:"creatorId"`
	IDs             []int64 `json:"ids"`
	DefaultPlaceIDs []int64 `json:"defaultPlaceIds"`
	PluginVersion   string  `json:"pluginVersion"`
	AssetType       string  `json:"assetType"`
	ExportJSON      bool    `json:"exportJSON"`
	IsGroup         bool    `json:"isGroup"`

	Products []ProductInfo `json:"products"`

	ShareUserID  int64 `json:"shareUserId"`
	ShareGroupID int64 `json:"shareGroupId"`
}

type Request struct {
	UniverseID      int64
	PlaceID         int64
	CreatorID       int64
	IDs             []int64
	DefaultPlaceIDs []int64
	IsGroup         bool
	Products        []ProductInfo
	ShareUserID     int64
	ShareGroupID    int64
}

func FromRawRequest(c *roblox.Client, req *RawRequest) (*Request, error) {
	placeID := req.PlaceID

	placesInfo, err := games.MultiGetPlaceDetails(c, []int64{placeID})
	if err != nil {
		return nil, err
	}

	return &Request{
		UniverseID:      placesInfo[0].UniverseID,
		PlaceID:         placeID,
		CreatorID:       req.CreatorID,
		IDs:             req.IDs,
		DefaultPlaceIDs: req.DefaultPlaceIDs,
		IsGroup:         req.IsGroup,
		Products:        req.Products,
		ShareUserID:     req.ShareUserID,
		ShareGroupID:    req.ShareGroupID,
	}, nil
}
