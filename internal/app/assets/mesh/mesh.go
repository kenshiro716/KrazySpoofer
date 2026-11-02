// ts barely works vro, plugin needs sum wokr
package mesh

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"krazyspoofer/internal/app/assets/shared/assetutils"
	"krazyspoofer/internal/app/assets/shared/clientutils"
	"krazyspoofer/internal/app/assets/shared/uploaderror"
	"krazyspoofer/internal/app/context"
	"krazyspoofer/internal/app/request"
	"krazyspoofer/internal/app/response"
	"krazyspoofer/internal/app/settings"
	"krazyspoofer/internal/retry"
	"krazyspoofer/internal/roblox/assetdelivery"
	"krazyspoofer/internal/roblox/develop"
	"krazyspoofer/internal/roblox/ide"
	"krazyspoofer/internal/taskqueue"
)

const assetTypeID int32 = 4

// Meshes use the same create endpoint as animations, so they share its limits.
const meshStartsPerMinute = 420
const meshMaxConcurrentUploads = 24
const meshUploadRetryTries = 5
const meshRateLimitMinBackoff = 2 * time.Second
const meshRateLimitMaxBackoff = time.Minute

var ErrUnauthorized = errors.New("authentication required to access asset")

func Reupload(ctx *context.Context, r *request.Request) {
	client := ctx.Client
	logger := ctx.Logger
	pauseController := ctx.PauseController
	resp := ctx.Response

	idsToUpload := len(r.IDs)
	var idsProcessed atomic.Int32

	var groupID int64
	if r.IsGroup {
		groupID = r.CreatorID
	}

	filter := assetutils.NewFilter(ctx, r, assetTypeID)
	uploadQueue := taskqueue.NewSmoothQueue[int64](meshMaxConcurrentUploads, meshStartsPerMinute)

	logger.Println("Reuploading meshes...")

	newBatchError := func(amt int, m string, err any) {
		end := int(idsProcessed.Add(int32(amt)))
		start := end - amt
		logger.Error(uploaderror.NewBatch(start, end, idsToUpload, m, err))
	}

	newUploadError := func(m string, assetInfo *develop.AssetInfo, err any) {
		newValue := idsProcessed.Add(1)
		logger.Error(uploaderror.New(int(newValue), idsToUpload, m, assetInfo, err))
	}

	uploadAsset := func(wg *sync.WaitGroup, assetInfo *develop.AssetInfo, location string) {
		defer wg.Done()

		oldName := assetInfo.Name

		assetData, err := clientutils.GetRequest(client, location)
		if err != nil {
			newUploadError("Failed to get asset data", assetInfo, err)
			return
		}

		uploadHandler, err := ide.NewUploadMeshHandler(client, assetInfo.Name, settings.AssetDescription, assetData, groupID)
		if err != nil {
			newUploadError("Failed to get upload handler", assetInfo, err)
			return
		}

		res := <-uploadQueue.QueueTask(func() (int64, error) {
			return retry.Do(
				retry.NewOptions(retry.Tries(meshUploadRetryTries)),
				func(try int) (int64, error) {
					pauseController.WaitIfPaused()
					if try > 1 {
						uploadQueue.Limiter.Wait()
					}

					id, err := uploadHandler()
					if err == nil {
						return id, nil
					}

					if err == ide.UploadMeshErrors.ErrNotLoggedIn {
						clientutils.GetNewCookie(ctx, r, "cookie expired")
					} else if err == ide.UploadMeshErrors.ErrInappropriateName {
						assetInfo.Name = fmt.Sprintf("(%s) [Censored]", assetInfo.Name)
					} else if errors.Is(err, ide.ErrRateLimited) {
						wait := meshRateLimitMinBackoff
						var rle *ide.RateLimitError
						if errors.As(err, &rle) && rle.RetryAfter > wait {
							wait = rle.RetryAfter
						}
						wait = min(wait, meshRateLimitMaxBackoff)
						uploadQueue.Chill(wait)
						time.Sleep(wait)
					}

					return 0, &retry.ContinueRetry{Err: err}
				},
			)
		})

		if err := res.Error; err != nil {
			assetInfo.Name = oldName
			newUploadError("Failed to upload", assetInfo, err)
			return
		}

		newID := res.Result
		newValue := idsProcessed.Add(1)
		logger.Success(uploaderror.New(int(newValue), idsToUpload, "", assetInfo, newID))
		resp.AddItem(response.ResponseItem{
			OldID: assetInfo.ID,
			NewID: newID,
		})
	}

	batchProcess := func(wg *sync.WaitGroup, res assetutils.AssetsInfoResult, batchSize int) {
		defer wg.Done()
		assetsInfo := res.Result

		if err := res.Error; err != nil {
			newBatchError(batchSize, "Failed to get assets info", err)
			return
		}

		filteredInfo := filter(assetsInfo)
		filteredInfoLength := len(filteredInfo)
		idsProcessed.Add(int32(batchSize - filteredInfoLength))
		if len(filteredInfo) == 0 {
			return
		}

		ids := make([]int64, filteredInfoLength)
		for i, assetInfo := range filteredInfo {
			ids[i] = assetInfo.ID
		}
		body := assetutils.NewBatchBodyFromIDs(ids)

		handler, err := assetdelivery.NewBatchHandler(client, body)
		if err != nil {
			newBatchError(filteredInfoLength, "Failed to get batch asset delivery handler", err)
			return
		}

		assetLocations, err := retry.Do(
			retry.NewOptions(retry.Tries(3)),
			func(_ int) ([]*assetdelivery.AssetLocation, error) {
				pauseController.WaitIfPaused()

				locations, err := handler()
				if err != nil {
					return locations, &retry.ContinueRetry{Err: err}
				}

				for _, assetLocation := range locations {
					errs := assetLocation.Errors
					if errs == nil {
						continue
					}
					if errs[0].Message == "Authentication required to access Asset." {
						clientutils.GetNewCookie(ctx, r, "cookie expired")
						return locations, &retry.ContinueRetry{Err: ErrUnauthorized}
					}
				}

				return locations, nil
			},
		)
		if err != nil {
			newBatchError(filteredInfoLength, "Failed to get asset locations", err)
			return
		}

		var uploadWG sync.WaitGroup
		uploadWG.Add(filteredInfoLength)
		for i, assetInfo := range filteredInfo {
			locationInfo := assetLocations[i]

			if errors := locationInfo.Errors; errors != nil {
				newUploadError("Failed to get asset location for", assetInfo, errors[0].Message)
				uploadWG.Done()
				continue
			}

			if len(locationInfo.Locations) == 0 {
				newUploadError("Failed to get asset location for", assetInfo, "no download URL returned")
				uploadWG.Done()
				continue
			}

			location := locationInfo.Locations[0].Location
			go uploadAsset(&uploadWG, assetInfo, location)
		}
		uploadWG.Wait()
	}

	var wg sync.WaitGroup
	tasks := assetutils.GetAssetsInfoInChunks(ctx, r)
	wg.Add(len(tasks))
	for i, task := range tasks {
		batchSize := 50
		if i == len(tasks)-1 {
			batchSize = idsToUpload % 50
			if batchSize == 0 {
				batchSize = 50
			}
		}

		go batchProcess(&wg, <-task, batchSize)
	}
	wg.Wait()
}
