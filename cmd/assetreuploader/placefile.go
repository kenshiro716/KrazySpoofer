package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kenshiro716/KrazySpoofer/internal/app/assets"
	"github.com/kenshiro716/KrazySpoofer/internal/app/config"
	"github.com/kenshiro716/KrazySpoofer/internal/app/request"
	"github.com/kenshiro716/KrazySpoofer/internal/app/response"
	"github.com/kenshiro716/KrazySpoofer/internal/color"
	"github.com/kenshiro716/KrazySpoofer/internal/console"
	"github.com/kenshiro716/KrazySpoofer/internal/placefile"
	"github.com/kenshiro716/KrazySpoofer/internal/roblox"
	"github.com/kenshiro716/KrazySpoofer/internal/roblox/games"
)

var placeFileAssetTypes = []string{"Animation", "Sound", "Mesh"}

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

func newPlaceFileRequest(c *roblox.Client, ids []int64) (*request.RawRequest, error) {
	placeID, err := promptID("Place id to reupload for: ", true)
	if err != nil {
		return nil, err
	}

	details, err := games.MultiGetPlaceDetails(c, []int64{placeID})
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

	shareUserID, err := promptID("Share with user id (Enter to skip): ", false)
	if err != nil {
		return nil, err
	}
	shareGroupID, err := promptID("Share with group id (Enter to skip): ", false)
	if err != nil {
		return nil, err
	}

	return &request.RawRequest{
		PlaceID:      placeID,
		CreatorID:    creatorID,
		IsGroup:      isGroup,
		IDs:          ids,
		ShareUserID:  shareUserID,
		ShareGroupID: shareGroupID,
	}, nil
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

func runPlaceFile(c *roblox.Client, path string) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".rbxlx":
	case ".rbxl":
		return errors.New("binary .rbxl files are not supported. In Studio use File > Save to File As, choose .rbxlx, then drop that file here")
	default:
		return errors.New("place file must be .rbxlx")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	ids, unreadable := placefile.Candidates(data)
	fmt.Printf("Found %d possible ids in %s\n", len(ids), filepath.Base(path))
	if unreadable > 0 {
		color.Warn.Println(unreadable, " attribute blobs could not be read and will not change.")
	}
	if len(ids) == 0 {
		return errors.New("no ids found")
	}

	rawRequest, err := newPlaceFileRequest(c, ids)
	if err != nil {
		return err
	}

	resp := response.New()
	replacements := make(map[int64]int64)
	for _, assetType := range placeFileAssetTypes {
		rawRequest.AssetType = assetType
		reupload, err := assets.NewReuploadHandlerWithType(assetType, c, rawRequest, resp)
		if err != nil {
			return err
		}
		if err := reupload(); err != nil {
			color.Error.Println("Failed to reupload ", assetType, ": ", err)
		}

		for _, item := range resp.Items() {
			replacements[item.OldID] = item.NewID
		}
		resp.Clear()
	}

	if len(replacements) == 0 {
		fmt.Println("Nothing was reuploaded, so no new file was saved.")
		return nil
	}

	result := placefile.Replace(data, replacements)
	output := placeFileOutputPath(path)
	if err := os.WriteFile(output, result.Data, 0o644); err != nil {
		return err
	}

	color.Success.Println("Reuploaded ", len(replacements), " assets and changed ", result.Replaced, " references.")
	fmt.Println("Saved to " + output)
	return nil
}
