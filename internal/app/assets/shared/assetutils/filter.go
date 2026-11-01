package assetutils

import (
	"fmt"

	"github.com/kenshiro716/KrazySpoofer/internal/app/context"
	"github.com/kenshiro716/KrazySpoofer/internal/app/request"
	"github.com/kenshiro716/KrazySpoofer/internal/roblox/develop"
)

func NewFilter(ctx *context.Context, r *request.Request, assetTypeID int32) func(assetsInfo develop.GetAssetsInfoResponse) []*develop.AssetInfo {
	creatorID := r.CreatorID
	userID := ctx.Client.UserInfo.ID
	checkUserID := !r.IsGroup

	return func(assetsInfo develop.GetAssetsInfoResponse) []*develop.AssetInfo {
		filteredAssetsInfo := assetsInfo.Data[:0]
		for _, info := range assetsInfo.Data {
			if info.TypeID != assetTypeID {
				continue
			}

			assetCreatorID := info.Creator.TargetID
			if assetCreatorID == 1 {
				continue
			}
			if assetCreatorID == creatorID || (checkUserID && assetCreatorID == userID) {
				ctx.Logger.Info(fmt.Sprintf("Already owned, skipped: %s(%d)", info.Name, info.ID))
				continue
			}

			filteredAssetsInfo = append(filteredAssetsInfo, info)
		}
		return filteredAssetsInfo
	}
}
