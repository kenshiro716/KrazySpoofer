package assets

import (
	"errors"
	"fmt"

	"krazyspoofer/internal/app/assets/animation"
	"krazyspoofer/internal/app/assets/developerproduct"
	"krazyspoofer/internal/app/assets/mesh"
	"krazyspoofer/internal/app/assets/shared/assetutils"
	"krazyspoofer/internal/app/assets/shared/clientutils"
	"krazyspoofer/internal/app/assets/shared/permissions"
	"krazyspoofer/internal/app/assets/sound"
	"krazyspoofer/internal/app/context"
	"krazyspoofer/internal/app/request"
	"krazyspoofer/internal/app/response"
	"krazyspoofer/internal/app/usage"
	"krazyspoofer/internal/console"
	"krazyspoofer/internal/roblox"
)

var assetModules = map[string]func(ctx *context.Context, r *request.Request){
	"Animation": animation.Reupload,
	"Mesh":      mesh.Reupload,
	"Sound":     sound.Reupload,

	"DeveloperProduct": developerproduct.Reupload,
	"GamePass":         developerproduct.ReuploadGamePasses,
}

func NewReuploadHandlerWithType(assetType string, c *roblox.Client, r *request.RawRequest, resp *response.Response) (func() error, error) {
	reupload, exists := assetModules[assetType]
	if !exists {
		return func() error { return nil }, errors.New(assetType + " module does not exist")
	}

	return func() error {
		ctx := context.New(c, resp)

		console.ClearScreen()

		fmt.Println("Getting current place details...")
		req, err := request.FromRawRequest(c, r)
		console.ClearScreen()
		if err != nil {
			return err
		}

		if req.UniverseID != 0 {
			fmt.Println("Checking if account can edit universe...")
			err = permissions.CanEditUniverse(ctx, req)
			console.ClearScreen()
			if err != nil {
				clientutils.GetNewCookie(ctx, req, err.Error())
			}
		}

		reupload(ctx, req)

		// Products and passes are not assets, so they have no permissions to share.
		newIDs := resp.TakeNewIDs()
		usage.Add(assetType, len(newIDs))
		if assetType != "DeveloperProduct" && assetType != "GamePass" {
			assetutils.ShareAssets(ctx, req, newIDs)
		}
		return nil
	}, nil
}

func DoesModuleExist(m string) bool {
	_, exists := assetModules[m]
	return exists
}
