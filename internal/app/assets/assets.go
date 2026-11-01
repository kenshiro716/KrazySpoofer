package assets

import (
	"errors"
	"fmt"

	"github.com/kenshiro716/KrazySpoofer/internal/app/assets/animation"
	"github.com/kenshiro716/KrazySpoofer/internal/app/assets/developerproduct"
	"github.com/kenshiro716/KrazySpoofer/internal/app/assets/mesh"
	"github.com/kenshiro716/KrazySpoofer/internal/app/assets/shared/assetutils"
	"github.com/kenshiro716/KrazySpoofer/internal/app/assets/shared/clientutils"
	"github.com/kenshiro716/KrazySpoofer/internal/app/assets/shared/permissions"
	"github.com/kenshiro716/KrazySpoofer/internal/app/assets/sound"
	"github.com/kenshiro716/KrazySpoofer/internal/app/context"
	"github.com/kenshiro716/KrazySpoofer/internal/app/request"
	"github.com/kenshiro716/KrazySpoofer/internal/app/response"
	"github.com/kenshiro716/KrazySpoofer/internal/console"
	"github.com/kenshiro716/KrazySpoofer/internal/roblox"
)

var assetModules = map[string]func(ctx *context.Context, r *request.Request){
	"Animation": animation.Reupload,
	"Mesh":      mesh.Reupload,
	"Sound":     sound.Reupload,

	"DeveloperProduct": developerproduct.Reupload,
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

		fmt.Println("Checking if account can edit universe...")
		err = permissions.CanEditUniverse(ctx, req)
		console.ClearScreen()
		if err != nil {
			clientutils.GetNewCookie(ctx, req, err.Error())
		}

		reupload(ctx, req)

		// Developer products are not assets, so they have no permissions to share.
		newIDs := resp.TakeNewIDs()
		if assetType != "DeveloperProduct" {
			assetutils.ShareAssets(ctx, req, newIDs)
		}
		return nil
	}, nil
}

func DoesModuleExist(m string) bool {
	_, exists := assetModules[m]
	return exists
}
