package roblox

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const cookieWarning = "WARNING:-DO-NOT-SHARE-THIS.--Sharing-this-will-allow-someone-to-log-in-as-you-and-to-steal-your-ROBUX-and-items."

var (
	ErrNoWarning = errors.New("include the .ROBLOSECURITY warning")
	ErrCancelled = errors.New("cancelled")
)

type Client struct {
	Cookie   string
	UserInfo UserInfo

	httpClient *http.Client

	token      string
	tokenMutex sync.RWMutex

	cancelled atomic.Bool
}

func NewClient(cookie string) (*Client, error) {
	c := &Client{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			// Uploads run dozens of requests at once against a few hosts. The default
			// of 2 idle connections per host forces a new TLS handshake for most of them.
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				ForceAttemptHTTP2:   true,
				MaxIdleConns:        200,
				MaxIdleConnsPerHost: 64,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
			},
		},
	}

	if err := c.SetCookie(cookie); err != nil {
		return c, err
	}

	return c, nil
}

func (c *Client) SetCookie(cookie string) error {
	c.Cookie = strings.TrimSpace(cookie)

	if !strings.Contains(cookie, cookieWarning) {
		return ErrNoWarning
	}

	userInfo, err := authenticate(c, cookie)
	if err != nil {
		return err
	}

	c.UserInfo = userInfo
	c.Cookie = cookie
	return nil
}

func (c *Client) GetToken() string {
	c.tokenMutex.RLock()
	defer c.tokenMutex.RUnlock()
	return c.token
}

func (c *Client) SetToken(s string) {
	c.tokenMutex.Lock()
	c.token = s
	c.tokenMutex.Unlock()
}

// SetCancelled makes every request fail straight away, which ends a run quickly.
func (c *Client) SetCancelled(cancelled bool) {
	c.cancelled.Store(cancelled)
}

func (c *Client) Cancelled() bool {
	return c.cancelled.Load()
}

func (c *Client) DoRequest(req *http.Request) (*http.Response, error) {
	if c.Cancelled() {
		return nil, ErrCancelled
	}
	return c.httpClient.Do(req)
}
