package assetutils

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"krazyspoofer/internal/app/assets/shared/clientutils"
	"krazyspoofer/internal/app/context"
	"krazyspoofer/internal/app/request"
	"krazyspoofer/internal/roblox/assets"
)

const (
	shareTries    = 3
	shareInterval = time.Second // Stays under the permissions API rate limit.
)

type shareTarget struct {
	subjectType string
	id          int64
}

func newShareBody(target shareTarget) assets.PermissionRequest {
	return assets.PermissionRequest{
		Requests: []assets.PermissionRequestItem{{
			SubjectType: target.subjectType,
			SubjectID:   strconv.FormatInt(target.id, 10),
			Action:      "Use",
		}},
	}
}

func shareAsset(ctx *context.Context, r *request.Request, assetID int64, target shareTarget) error {
	body := newShareBody(target)

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

// ShareAssets gives the request's share user and group permission to use each asset.
func ShareAssets(ctx *context.Context, r *request.Request, assetIDs []int64) {
	if len(assetIDs) == 0 {
		return
	}

	if r.ShareUserID == ctx.Client.UserInfo.ID {
		ctx.Logger.Warn("Not sharing with the user id: it is the logged in account.")
	} else if r.ShareUserID != 0 {
		shareWithTarget(ctx, r, assetIDs, shareTarget{"User", r.ShareUserID})
	}

	if r.ShareGroupID != 0 {
		shareWithTarget(ctx, r, assetIDs, shareTarget{"Group", r.ShareGroupID})
	}
}

func shareWithTarget(ctx *context.Context, r *request.Request, assetIDs []int64, target shareTarget) {
	logger := ctx.Logger
	logger.Println(fmt.Sprintf("Sharing %d assets with %s %d...", len(assetIDs), target.subjectType, target.id))

	shared := 0
	for i, assetID := range assetIDs {
		if ctx.Client.Cancelled() {
			return
		}
		if i > 0 {
			time.Sleep(shareInterval)
		}

		if err := shareAsset(ctx, r, assetID, target); err != nil {
			logger.Error(fmt.Sprintf("[%d/%d] Failed to share %d: %v", i+1, len(assetIDs), assetID, err))
			continue
		}

		shared++
		logger.Success(fmt.Sprintf("[%d/%d] Shared %d", i+1, len(assetIDs), assetID))
	}

	logger.Println(fmt.Sprintf("Shared %d/%d assets with %s %d.", shared, len(assetIDs), target.subjectType, target.id))
}
