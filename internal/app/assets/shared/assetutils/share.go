package assetutils

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kenshiro716/Asset-Reuploader/internal/app/assets/shared/clientutils"
	"github.com/kenshiro716/Asset-Reuploader/internal/app/context"
	"github.com/kenshiro716/Asset-Reuploader/internal/app/request"
	"github.com/kenshiro716/Asset-Reuploader/internal/roblox/assets"
)

const (
	shareTries    = 3
	shareInterval = time.Second // Stays under the permissions API rate limit.
)

func newShareBody(userID int64) assets.PermissionRequest {
	return assets.PermissionRequest{
		Requests: []assets.PermissionRequestItem{{
			SubjectType: "User",
			SubjectID:   strconv.FormatInt(userID, 10),
			Action:      "Use",
		}},
	}
}

func shareAsset(ctx *context.Context, r *request.Request, assetID int64) error {
	body := newShareBody(r.ShareUserID)

	var lastErr error
	for try := 1; try <= shareTries; try++ {
		ctx.PauseController.WaitIfPaused()

		// A request's body can only be read once, so every try needs a new one.
		handler, err := assets.NewUpdatePermissionsHandler(ctx.Client, assetID, body)
		if err != nil {
			return err
		}

		response, err := handler()
		if err == nil {
			if len(response.Errors) > 0 {
				return errors.New(response.Errors[0].Message)
			}
			return nil
		}
		lastErr = err

		switch {
		case errors.Is(err, assets.UpdatePermissionErrors.ErrTokenInvalid):
		case errors.Is(err, assets.UpdatePermissionErrors.ErrNotAuthenticated):
			clientutils.GetNewCookie(ctx, r, "cookie expired")
		case strings.Contains(err.Error(), "429"):
			time.Sleep(time.Duration(try) * 5 * time.Second)
		default:
			return err
		}
	}
	return lastErr
}

// ShareWithUser gives r.ShareUserID permission to use each asset.
func ShareWithUser(ctx *context.Context, r *request.Request, assetIDs []int64) {
	logger := ctx.Logger

	if len(assetIDs) == 0 {
		return
	}
	if r.ShareUserID == ctx.Client.UserInfo.ID {
		logger.Warn("Not sharing: the share user id is the logged in account.")
		return
	}

	logger.Println(fmt.Sprintf("Sharing %d assets with user %d...", len(assetIDs), r.ShareUserID))

	shared := 0
	for i, assetID := range assetIDs {
		if i > 0 {
			time.Sleep(shareInterval)
		}

		if err := shareAsset(ctx, r, assetID); err != nil {
			logger.Error(fmt.Sprintf("[%d/%d] Failed to share %d: %v", i+1, len(assetIDs), assetID, err))
			continue
		}

		shared++
		logger.Success(fmt.Sprintf("[%d/%d] Shared %d", i+1, len(assetIDs), assetID))
	}

	logger.Println(fmt.Sprintf("Shared %d/%d assets with user %d.", shared, len(assetIDs), r.ShareUserID))
}
