package developerproduct

import (
	"errors"
	"fmt"
	"time"

	"github.com/kenshiro716/KrazySpoofer/internal/app/assets/shared/clientutils"
	"github.com/kenshiro716/KrazySpoofer/internal/app/assets/shared/uploaderror"
	"github.com/kenshiro716/KrazySpoofer/internal/app/context"
	"github.com/kenshiro716/KrazySpoofer/internal/app/request"
	"github.com/kenshiro716/KrazySpoofer/internal/app/response"
	"github.com/kenshiro716/KrazySpoofer/internal/roblox/develop"
	"github.com/kenshiro716/KrazySpoofer/internal/roblox/developerproducts"
)

const (
	maxTries     = 5
	maxNameTries = 5
	maxWait      = time.Minute
)

// withRetry runs f, waiting out rate limits and asking for a new cookie when
// the current one has expired.
func withRetry[T any](ctx *context.Context, r *request.Request, f func() (T, error)) (T, error) {
	var zero T
	var lastErr error
	for try := 1; try <= maxTries; try++ {
		ctx.PauseController.WaitIfPaused()

		result, err := f()
		if err == nil {
			return result, nil
		}
		lastErr = err

		var rle *developerproducts.RateLimitError
		switch {
		case errors.As(err, &rle):
			time.Sleep(min(rle.RetryAfter, maxWait))
		case errors.Is(err, developerproducts.ErrNotLoggedIn):
			clientutils.GetNewCookie(ctx, r, "cookie expired")
		case errors.Is(err, developerproducts.ErrNameConflict):
			return zero, err
		default:
			time.Sleep(time.Duration(try) * time.Second)
		}
	}
	return zero, lastErr
}

func Reupload(ctx *context.Context, r *request.Request) {
	client := ctx.Client
	logger := ctx.Logger
	resp := ctx.Response

	products := make([]request.ProductInfo, 0, len(r.Products))
	seen := make(map[int64]struct{}, len(r.Products))
	for _, p := range r.Products {
		if _, dup := seen[p.ID]; dup || p.ID == 0 {
			continue
		}
		seen[p.ID] = struct{}{}
		products = append(products, p)
	}
	total := len(products)

	logger.Println("Reuploading developer products...")

	existing, err := withRetry(ctx, r, func() ([]developerproducts.Product, error) {
		return developerproducts.List(client, r.UniverseID)
	})
	if err != nil {
		logger.Warn("Could not list this game's developer products, duplicates may be created: ", err)
		existing = nil
	}

	existingIDs := make(map[int64]struct{}, len(existing))
	existingByName := make(map[string]int64, len(existing))
	for _, p := range existing {
		existingIDs[p.ProductID] = struct{}{}
		existingByName[p.Name] = p.ProductID
	}

	for i, p := range products {
		done := i + 1
		info := &develop.AssetInfo{ID: p.ID, Name: p.Name}

		if _, ok := existingIDs[p.ID]; ok {
			logger.Info(fmt.Sprintf("[%d/%d] %s(%d) already belongs to this game, skipped", done, total, p.Name, p.ID))
			continue
		}

		if id, ok := existingByName[p.Name]; ok {
			resp.AddItem(response.ResponseItem{OldID: p.ID, NewID: id})
			logger.Success(uploaderror.New(done, total, "reused existing", info, id))
			continue
		}

		var icon []byte
		if p.IconImageAssetID != 0 {
			icon, err = developerproducts.DownloadIcon(client, p.IconImageAssetID)
			if err != nil {
				logger.Warn(fmt.Sprintf(">> %s(%d) icon not copied: %v", p.Name, p.ID, err))
				icon = nil
			}
		}

		price := p.Price
		if price < 1 {
			price = 1
			logger.Warn(fmt.Sprintf(">> %s(%d) had no price, set to 1 Robux", p.Name, p.ID))
		}

		name := p.Name
		var created developerproducts.Product
		for attempt := 1; ; attempt++ {
			created, err = withRetry(ctx, r, func() (developerproducts.Product, error) {
				return developerproducts.Create(client, r.UniverseID, name, p.Description, price, icon)
			})
			if !errors.Is(err, developerproducts.ErrNameConflict) || attempt >= maxNameTries {
				break
			}
			name = fmt.Sprintf("%s (%d)", p.Name, attempt+1)
		}
		if err != nil {
			logger.Error(uploaderror.New(done, total, "Failed to create", info, err))
			continue
		}

		existingByName[created.Name] = created.ProductID
		resp.AddItem(response.ResponseItem{OldID: p.ID, NewID: created.ProductID})
		logger.Success(uploaderror.New(done, total, "", info, created.ProductID))
	}
}
