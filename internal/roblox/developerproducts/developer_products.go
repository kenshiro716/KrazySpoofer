package developerproducts

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kenshiro716/KrazySpoofer/internal/app/config"
	"github.com/kenshiro716/KrazySpoofer/internal/roblox"
)

var baseURL = "https://apis.roblox.com/developer-products/v2/universes/"

var (
	ErrNotLoggedIn  = errors.New("not logged in")
	ErrRateLimited  = errors.New("rate limited")
	ErrNameConflict = errors.New("a developer product with this name already exists")
)

// RateLimitError is returned on HTTP 429. errors.Is(err, ErrRateLimited) is true.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string { return "rate limited" }

func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

// flexInt accepts both JSON numbers and numeric strings; the API docs show both.
type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return err
	}
	*f = flexInt(v)
	return nil
}

type Product struct {
	ProductID        int64
	Name             string
	Description      string
	IconImageAssetID int64
}

type productJSON struct {
	ProductID        flexInt `json:"productId"`
	Name             string  `json:"name"`
	Description      string  `json:"description"`
	IconImageAssetID flexInt `json:"iconImageAssetId"`
}

func (p productJSON) toProduct() Product {
	return Product{
		ProductID:        int64(p.ProductID),
		Name:             p.Name,
		Description:      p.Description,
		IconImageAssetID: int64(p.IconImageAssetID),
	}
}

type listResponse struct {
	DeveloperProducts []productJSON `json:"developerProducts"`
	NextPageToken     string        `json:"nextPageToken"`
}

// do sends an authenticated request built by newReq. It refreshes the XSRF
// token once, and if an API key without developer-product scopes is
// rejected, retries once with the cookie alone.
func do(c *roblox.Client, newReq func() (*http.Request, error)) (int, []byte, error) {
	apiKey := strings.TrimSpace(config.Get("api_key"))
	useAPIKey := apiKey != ""
	refreshedToken := false

	for {
		req, err := newReq()
		if err != nil {
			return 0, nil, err
		}
		req.AddCookie(&http.Cookie{Name: ".ROBLOSECURITY", Value: c.Cookie})
		req.Header.Set("x-csrf-token", c.GetToken())
		req.Header.Set("User-Agent", "RobloxStudio/WinInet")
		if useAPIKey {
			req.Header.Set("x-api-key", apiKey)
		}

		resp, err := c.DoRequest(req)
		if err != nil {
			return 0, nil, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return 0, nil, err
		}

		if resp.StatusCode == http.StatusForbidden {
			if token := resp.Header.Get("x-csrf-token"); token != "" && !refreshedToken {
				c.SetToken(token)
				refreshedToken = true
				continue
			}
		}
		if (resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) && useAPIKey {
			useAPIKey = false
			continue
		}

		switch resp.StatusCode {
		case http.StatusOK:
			return resp.StatusCode, body, nil
		case http.StatusUnauthorized:
			return resp.StatusCode, body, ErrNotLoggedIn
		case http.StatusTooManyRequests:
			wait := 5 * time.Second
			if sec, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && sec > 0 {
				wait = time.Duration(sec) * time.Second
			}
			return resp.StatusCode, body, &RateLimitError{RetryAfter: wait}
		default:
			return resp.StatusCode, body, fmt.Errorf("%s: %s", resp.Status, errorMessage(body))
		}
	}
}

func errorMessage(body []byte) string {
	var e struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &e) == nil {
		if e.Message != "" {
			return e.Message
		}
		if len(e.Errors) > 0 && e.Errors[0].Message != "" {
			return e.Errors[0].Message
		}
	}
	s := strings.TrimSpace(string(body))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// List returns every developer product in a universe the client can edit.
func List(c *roblox.Client, universeID int64) ([]Product, error) {
	products := make([]Product, 0)
	pageToken := ""
	for {
		q := url.Values{"pageSize": {"50"}}
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		u := fmt.Sprintf("%s%d/developer-products/creator?%s", baseURL, universeID, q.Encode())

		_, body, err := do(c, func() (*http.Request, error) {
			return http.NewRequest("GET", u, http.NoBody)
		})
		if err != nil {
			return nil, err
		}

		var page listResponse
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, err
		}
		for _, p := range page.DeveloperProducts {
			products = append(products, p.toProduct())
		}

		if page.NextPageToken == "" || len(page.DeveloperProducts) == 0 {
			return products, nil
		}
		pageToken = page.NextPageToken
	}
}

// Create makes a new developer product in a universe and returns it.
// icon may be nil to create the product without an image.
func Create(c *roblox.Client, universeID int64, name, description string, price int64, icon []byte) (Product, error) {
	u := fmt.Sprintf("%s%d/developer-products", baseURL, universeID)

	_, body, err := do(c, func() (*http.Request, error) {
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
		lowered := strings.ToLower(err.Error())
		if strings.Contains(lowered, "409") || strings.Contains(lowered, "already exist") || strings.Contains(lowered, "duplicate") {
			return Product{}, ErrNameConflict
		}
		return Product{}, err
	}

	var p productJSON
	if err := json.Unmarshal(body, &p); err != nil {
		return Product{}, err
	}
	if p.ProductID == 0 {
		return Product{}, fmt.Errorf("create response had no productId: %s", errorMessage(body))
	}
	return p.toProduct(), nil
}

// DownloadIcon fetches the image behind an icon asset id.
func DownloadIcon(c *roblox.Client, iconAssetID int64) ([]byte, error) {
	u := fmt.Sprintf("https://assetdelivery.roblox.com/v1/asset/?id=%d", iconAssetID)
	req, err := http.NewRequest("GET", u, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.AddCookie(&http.Cookie{Name: ".ROBLOSECURITY", Value: c.Cookie})
	req.Header.Set("User-Agent", "RobloxStudio/WinInet")

	resp, err := c.DoRequest(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 20<<20))
}
