package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kenshiro716/KrazySpoofer/internal/app/assets"
	"github.com/kenshiro716/KrazySpoofer/internal/app/assets/shared/assetutils"
	"github.com/kenshiro716/KrazySpoofer/internal/app/assets/shared/permissions"
	"github.com/kenshiro716/KrazySpoofer/internal/app/config"
	"github.com/kenshiro716/KrazySpoofer/internal/app/context"
	"github.com/kenshiro716/KrazySpoofer/internal/app/request"
	"github.com/kenshiro716/KrazySpoofer/internal/app/response"
	"github.com/kenshiro716/KrazySpoofer/internal/color"
	"github.com/kenshiro716/KrazySpoofer/internal/console"
	"github.com/kenshiro716/KrazySpoofer/internal/placefile"
	"github.com/kenshiro716/KrazySpoofer/internal/roblox"
	"github.com/kenshiro716/KrazySpoofer/internal/roblox/developerproducts"
	"github.com/kenshiro716/KrazySpoofer/internal/roblox/gamepasses"
	"github.com/kenshiro716/KrazySpoofer/internal/roblox/games"
	"github.com/kenshiro716/KrazySpoofer/internal/roblox/publish"
)

const (
	productLookupWorkers = 4
	productLookupTries   = 6
)

var placeFileAssetTypes = []string{"Animation", "Sound", "Mesh"}

// placeJob is one reupload of a place file, from the prompts or the app.
type placeJob struct {
	PlaceID      int64
	ShareUserID  int64
	ShareGroupID int64
	AssetTypes   []string
	GamePasses   []request.ProductInfo
	Products     []request.ProductInfo
}

// placeFileArgument returns the place file dropped on the executable, if any.
func placeFileArgument() string {
	if !config.HasPlaceFileArgument() {
		return ""
	}
	if path := os.Args[1]; filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(config.StartDirectory(), os.Args[1])
}

func promptID(message string, required bool) (int64, error) {
	for {
		input, err := console.Input(message)
		if err != nil {
			return 0, err
		}
		if input == "" && !required {
			return 0, nil
		}
		if id, err := strconv.ParseInt(input, 10, 64); err == nil && id > 0 {
			return id, nil
		}
		color.Error.Println("Enter a number.")
	}
}

func placeFileOutputPath(path string) string {
	extension := filepath.Ext(path)
	base := strings.TrimSuffix(path, extension)

	output := base + ".reuploaded" + extension
	if _, err := os.Stat(output); errors.Is(err, os.ErrNotExist) {
		return output
	}
	return fmt.Sprintf("%s.reuploaded-%s%s", base, time.Now().Format("2006-01-02_15-04-05"), extension)
}

func newRawRequest(c *roblox.Client, job *placeJob, ids []int64) (*request.RawRequest, error) {
	details, err := games.MultiGetPlaceDetails(c, []int64{job.PlaceID})
	if err != nil {
		return nil, err
	}
	if len(details) == 0 {
		return nil, errors.New("place not found")
	}

	creatorID, isGroup, err := games.UniverseCreator(c, details[0].UniverseID)
	if err != nil {
		return nil, err
	}

	return &request.RawRequest{
		PlaceID:      job.PlaceID,
		CreatorID:    creatorID,
		IsGroup:      isGroup,
		IDs:          ids,
		ShareUserID:  job.ShareUserID,
		ShareGroupID: job.ShareGroupID,
		GamePasses:   job.GamePasses,
		Products:     job.Products,
	}, nil
}

func nonAssetIDs(c *roblox.Client, ids []int64) []int64 {
	ctx := context.New(c, response.New())
	r := &request.Request{IDs: ids}

	isAsset := make(map[int64]bool, len(ids))
	checked := make(map[int64]bool, len(ids))
	for i, task := range assetutils.GetAssetsInfoInChunks(ctx, r) {
		result := <-task
		if result.Error != nil {
			continue
		}
		start := i * assetutils.AssetsInfoChunkSize
		for _, id := range ids[start:min(start+assetutils.AssetsInfoChunkSize, len(ids))] {
			checked[id] = true
		}
		for _, info := range result.Result.Data {
			isAsset[info.ID] = true
		}
	}

	remaining := make([]int64, 0)
	for _, id := range ids {
		if checked[id] && !isAsset[id] {
			remaining = append(remaining, id)
		}
	}
	return remaining
}

// lookUp returns false if the id could not be checked at all.
func lookUp(get func() (*developerproducts.Details, error)) (*developerproducts.Details, bool) {
	for range productLookupTries {
		details, err := get()
		if err == nil {
			return details, true
		}

		var rateLimit *developerproducts.RateLimitError
		if !errors.As(err, &rateLimit) {
			return nil, false
		}
		time.Sleep(rateLimit.RetryAfter)
	}
	return nil, false
}

func toProductInfo(id int64, d *developerproducts.Details) request.ProductInfo {
	return request.ProductInfo{
		ID:               id,
		Name:             d.Name,
		Description:      d.Description,
		Price:            d.PriceInRobux,
		IconImageAssetID: d.IconImageAssetID,
		Creator:          d.Creator.Name,
	}
}

// findProducts checks which ids are game passes or developer products. Numbers
// in a game can match by chance, so the results need to be confirmed by the user.
func findProducts(c *roblox.Client, ids []int64, progress func(done, total int)) (passes, products []request.ProductInfo) {
	candidates := nonAssetIDs(c, ids)

	var mutex sync.Mutex
	var next, done, unchecked atomic.Int64
	var workers sync.WaitGroup

	for range productLookupWorkers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for !c.Cancelled() {
				index := int(next.Add(1)) - 1
				if index >= len(candidates) {
					return
				}
				id := candidates[index]

				pass, passChecked := lookUp(func() (*developerproducts.Details, error) { return gamepasses.GetDetails(c, id) })
				var product *developerproducts.Details
				productChecked := true
				if pass == nil {
					product, productChecked = lookUp(func() (*developerproducts.Details, error) { return developerproducts.GetDetails(c, id) })
				}
				if !passChecked || !productChecked {
					unchecked.Add(1)
				}

				mutex.Lock()
				if pass != nil {
					passes = append(passes, toProductInfo(id, pass))
				} else if product != nil {
					products = append(products, toProductInfo(id, product))
				}
				mutex.Unlock()

				if progress != nil {
					progress(int(done.Add(1)), len(candidates))
				}
			}
		}()
	}
	workers.Wait()

	if count := unchecked.Load(); count > 0 && !c.Cancelled() {
		color.Warn.Println(count, " ids could not be checked, so some products may be missing. Scan again to retry.")
	}
	return passes, products
}

// runPlaceJob reuploads everything in job and returns the old to new ids.
func runPlaceJob(c *roblox.Client, job *placeJob, ids []int64) (map[int64]int64, error) {
	rawRequest, err := newRawRequest(c, job, ids)
	if err != nil {
		return nil, err
	}

	// Checked here so a run with no access stops with a message instead of
	// waiting for a new cookie in the console.
	resp := response.New()
	req, err := request.FromRawRequest(c, rawRequest)
	if err != nil {
		return nil, err
	}
	if err := permissions.CanEditUniverse(context.New(c, resp), req); err != nil {
		return nil, fmt.Errorf("this account cannot edit that place: %w", err)
	}

	assetTypes := append([]string(nil), job.AssetTypes...)
	if len(job.GamePasses) > 0 {
		assetTypes = append(assetTypes, "GamePass")
	}
	if len(job.Products) > 0 {
		assetTypes = append(assetTypes, "DeveloperProduct")
	}

	replacements := make(map[int64]int64)
	for _, assetType := range assetTypes {
		if c.Cancelled() {
			break
		}

		rawRequest.AssetType = assetType
		reupload, err := assets.NewReuploadHandlerWithType(assetType, c, rawRequest, resp)
		if err != nil {
			return replacements, err
		}
		if err := reupload(); err != nil {
			color.Error.Println("Failed to reupload ", assetType, ": ", err)
		}

		for _, item := range resp.Items() {
			replacements[item.OldID] = item.NewID
		}
		resp.Clear()
	}
	return replacements, nil
}

// savePlaceFile writes a copy of data with the new ids in it.
func savePlaceFile(data []byte, replacements map[int64]int64, output string) (int, error) {
	result, err := placefile.Replace(data, replacements)
	if err != nil {
		return 0, err
	}
	return result.Replaced, os.WriteFile(output, result.Data, 0o644)
}

func printAudioQuota(c *roblox.Client) {
	if quota, err := publish.GetAudioQuota(c); err == nil {
		fmt.Printf("Audio uploads used: %d/%d\n", quota.Usage, quota.Capacity)
	}
}

func confirmProducts(kind string, items []request.ProductInfo) ([]request.ProductInfo, error) {
	if len(items) == 0 {
		return nil, nil
	}

	fmt.Printf("\nFound %d %s:\n", len(items), kind)
	for _, item := range items {
		fmt.Printf("  %s (%d) - %d Robux - by %s\n", item.Name, item.ID, item.Price, item.Creator)
	}

	answer, err := console.Input("Reupload all of these? Numbers can match by chance, so check the list. (y/n): ")
	if err != nil || !strings.EqualFold(answer, "y") {
		return nil, err
	}
	return items, nil
}

func runPlaceFile(c *roblox.Client, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	ids, unreadable, err := placefile.Candidates(data)
	if err != nil {
		return err
	}
	fmt.Printf("Found %d possible ids in %s\n", len(ids), filepath.Base(path))
	if unreadable > 0 {
		color.Warn.Println(unreadable, " attribute blobs could not be read and will not change.")
	}
	if len(ids) == 0 {
		return errors.New("no ids found")
	}
	printAudioQuota(c)

	job := &placeJob{AssetTypes: placeFileAssetTypes}
	if job.PlaceID, err = promptID("Place id to reupload for: ", true); err != nil {
		return err
	}
	if job.ShareUserID, err = promptID("Share with user id (Enter to skip): ", false); err != nil {
		return err
	}
	if job.ShareGroupID, err = promptID("Share with group id (Enter to skip): ", false); err != nil {
		return err
	}

	answer, err := console.Input("Look for game passes and developer products too? This can take a few minutes. (y/n): ")
	if err != nil {
		return err
	}
	if strings.EqualFold(answer, "y") {
		passes, products := findProducts(c, ids, nil)
		if job.GamePasses, err = confirmProducts("game passes", passes); err != nil {
			return err
		}
		if job.Products, err = confirmProducts("developer products", products); err != nil {
			return err
		}
	}

	replacements, err := runPlaceJob(c, job, ids)
	if err != nil {
		return err
	}
	if len(replacements) == 0 {
		fmt.Println("Nothing was reuploaded, so no new file was saved.")
		return nil
	}

	output := placeFileOutputPath(path)
	changed, err := savePlaceFile(data, replacements, output)
	if err != nil {
		return err
	}

	color.Success.Println("Reuploaded ", len(replacements), " items and changed ", changed, " references.")
	fmt.Println("Saved to " + output)
	return nil
}
