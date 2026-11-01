package assetutils

import (
	"fmt"
	"slices"

	"github.com/kenshiro716/KrazySpoofer/internal/app/context"
	"github.com/kenshiro716/KrazySpoofer/internal/app/request"
	"github.com/kenshiro716/KrazySpoofer/internal/app/settings"
	"github.com/kenshiro716/KrazySpoofer/internal/roblox/develop"
)

const creatorTypeGroup = "Group"

func NewFilter(ctx *context.Context, r *request.Request, assetTypeID int32) func(assetsInfo develop.GetAssetsInfoResponse) []*develop.AssetInfo {
	options := settings.Get()
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
			if options.SkipOwned && (assetCreatorID == creatorID || (checkUserID && assetCreatorID == userID)) {
				ctx.Logger.Info(fmt.Sprintf("Already owned, skipped: %s(%d)", info.Name, info.ID))
				continue
			}
			if options.SkipPublic && info.IsPublicDomainEnabled {
				ctx.Logger.Info(fmt.Sprintf("Public asset, skipped: %s(%d)", info.Name, info.ID))
				continue
			}

			skipList := options.SkipUserIDs
			if info.Creator.Type == creatorTypeGroup {
				skipList = options.SkipGroupIDs
			}
			if slices.Contains(skipList, assetCreatorID) {
				ctx.Logger.Info(fmt.Sprintf("Creator on skip list, skipped: %s(%d)", info.Name, info.ID))
				continue
			}

			filteredAssetsInfo = append(filteredAssetsInfo, info)
		}
		return filteredAssetsInfo
	}
}
