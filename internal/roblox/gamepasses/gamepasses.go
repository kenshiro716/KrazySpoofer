// Package gamepasses reads and creates game passes.
package gamepasses

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"

	"github.com/kenshiro716/KrazySpoofer/internal/roblox"
	"github.com/kenshiro716/KrazySpoofer/internal/roblox/developerproducts"
)

const baseURL = "https://apis.roblox.com/game-passes/v1/universes/"

type gamePass struct {
	GamePassID developerproducts.FlexInt `json:"gamePassId"`
	Name       string                    `json:"name"`
}

func (p gamePass) toProduct() developerproducts.Product {
	return developerproducts.Product{ProductID: int64(p.GamePassID), Name: p.Name}
}

// List returns every game pass in a universe the client can edit.
func List(c *roblox.Client, universeID int64) ([]developerproducts.Product, error) {
	products := make([]developerproducts.Product, 0)
	pageToken := ""
	for {
		query := url.Values{"pageSize": {"50"}}
		if pageToken != "" {
			query.Set("pageToken", pageToken)
		}
		u := fmt.Sprintf("%s%d/game-passes/creator?%s", baseURL, universeID, query.Encode())

		_, body, err := developerproducts.Do(c, func() (*http.Request, error) {
			return http.NewRequest("GET", u, http.NoBody)
		})
		if err != nil {
			return nil, err
		}

		var page struct {
			GamePasses    []gamePass `json:"gamePasses"`
			NextPageToken string     `json:"nextPageToken"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, err
		}
		for _, pass := range page.GamePasses {
			products = append(products, pass.toProduct())
		}

		if page.NextPageToken == "" || len(page.GamePasses) == 0 {
			return products, nil
		}
		pageToken = page.NextPageToken
	}
}

// Create makes a new game pass in a universe. icon may be nil.
func Create(c *roblox.Client, universeID int64, name, description string, price int64, icon []byte) (developerproducts.Product, error) {
	u := fmt.Sprintf("%s%d/game-passes", baseURL, universeID)

	_, body, err := developerproducts.Do(c, func() (*http.Request, error) {
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		_ = w.WriteField("name", name)
		_ = w.WriteField("description", description)
		_ = w.WriteField("price", strconv.FormatInt(price, 10))
		_ = w.WriteField("isForSale", "true")
		if len(icon) > 0 {
			part, err := w.CreateFormFile("imageFile", "icon.png")
			if err != nil {
				return nil, err
			}
			if _, err := part.Write(icon); err != nil {
				return nil, err
			}
		}
		if err := w.Close(); err != nil {
			return nil, err
		}

		req, err := http.NewRequest("POST", u, &buf)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", w.FormDataContentType())
		return req, nil
	})
	if err != nil {
		return developerproducts.Product{}, developerproducts.ClassifyCreateError(err)
	}

	var pass gamePass
	if err := json.Unmarshal(body, &pass); err != nil {
		return developerproducts.Product{}, err
	}
	if pass.GamePassID == 0 {
		return developerproducts.Product{}, fmt.Errorf("create response had no gamePassId")
	}
	return pass.toProduct(), nil
}

// GetDetails looks up a game pass by id.
func GetDetails(c *roblox.Client, id int64) (*developerproducts.Details, error) {
	u := fmt.Sprintf("https://apis.roblox.com/game-passes/v1/game-passes/%d/product-info", id)
	details, err := developerproducts.PublicDetails(c, u)
	if details != nil && details.ProductType != "Game Pass" {
		return nil, err
	}
	return details, err
}
