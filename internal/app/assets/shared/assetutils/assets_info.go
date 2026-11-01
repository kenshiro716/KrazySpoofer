package assetutils

import (
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/kenshiro716/KrazySpoofer/internal/app/assets/shared/clientutils"
	"github.com/kenshiro716/KrazySpoofer/internal/app/context"
	"github.com/kenshiro716/KrazySpoofer/internal/app/request"
	"github.com/kenshiro716/KrazySpoofer/internal/retry"
	"github.com/kenshiro716/KrazySpoofer/internal/roblox"
	"github.com/kenshiro716/KrazySpoofer/internal/roblox/develop"
	"github.com/kenshiro716/KrazySpoofer/internal/taskqueue"
)

const AssetsInfoChunkSize int = 50

const (
	// Asset info checks per minute, at up to 50 ids each.
	assetsInfoChunksPerMinute = 120
	assetsInfoTries           = 8
	assetsInfoRateLimitWait   = 10 * time.Second
)

type AssetsInfoResult = taskqueue.TaskResult[develop.GetAssetsInfoResponse]

// assetsInfoCache remembers checked ids, so one place file only checks each id
// once even though every asset type looks at all of them.
var assetsInfoCache = struct {
	sync.Mutex
	checked map[int64]bool
	info    map[int64]develop.AssetInfo
}{
	checked: make(map[int64]bool),
	info:    make(map[int64]develop.AssetInfo),
}

// ClearAssetsInfoCache forgets checked ids. Call it before a new run.
func ClearAssetsInfoCache() {
	assetsInfoCache.Lock()
	defer assetsInfoCache.Unlock()
	assetsInfoCache.checked = make(map[int64]bool)
	assetsInfoCache.info = make(map[int64]develop.AssetInfo)
}

// cachedAssetsInfo returns copies, because callers change what they are given.
func cachedAssetsInfo(ids []int64) (develop.GetAssetsInfoResponse, bool) {
	assetsInfoCache.Lock()
	defer assetsInfoCache.Unlock()

	response := develop.GetAssetsInfoResponse{Data: make([]*develop.AssetInfo, 0, len(ids))}
	for _, id := range ids {
		if !assetsInfoCache.checked[id] {
			return develop.GetAssetsInfoResponse{}, false
		}
		if info, exists := assetsInfoCache.info[id]; exists {
			response.Data = append(response.Data, &info)
		}
	}
	return response, true
}

func cacheAssetsInfo(ids []int64, response develop.GetAssetsInfoResponse) {
	assetsInfoCache.Lock()
	defer assetsInfoCache.Unlock()

	for _, id := range ids {
		assetsInfoCache.checked[id] = true
	}
	for _, info := range response.Data {
		assetsInfoCache.info[info.ID] = *info
	}
}

// rateLimitPause holds back every check at once after Roblox answers 429.
type rateLimitPause struct {
	sync.Mutex
	until time.Time
}

func (p *rateLimitPause) extend(wait time.Duration) {
	p.Lock()
	defer p.Unlock()
	if until := time.Now().Add(wait); until.After(p.until) {
		p.until = until
	}
}

func (p *rateLimitPause) wait() {
	p.Lock()
	until := p.until
	p.Unlock()
	time.Sleep(time.Until(until))
}

func GetAssetsInfoInChunks(ctx *context.Context, r *request.Request) []chan AssetsInfoResult {
	queue := taskqueue.New[develop.GetAssetsInfoResponse](time.Minute, assetsInfoChunksPerMinute)
	pause := &rateLimitPause{}

	newAssetsInfoHandler := func(ids []int64) func() (develop.GetAssetsInfoResponse, error) {
		return func() (develop.GetAssetsInfoResponse, error) {
			handler, err := develop.NewAssetsInfoHandler(ctx.Client, ids)
			if err != nil {
				return develop.GetAssetsInfoResponse{}, err
			}

			response, err := retry.Do(
				retry.NewOptions(retry.Tries(assetsInfoTries)),
				func(try int) (develop.GetAssetsInfoResponse, error) {
					ctx.PauseController.WaitIfPaused()
					pause.wait()
					if try > 1 {
						queue.Limiter.Wait()
					}

					assetsInfo, err := handler()
					if err == nil {
						return assetsInfo, nil
					}

					switch {
					case errors.Is(err, roblox.ErrCancelled):
						return develop.GetAssetsInfoResponse{}, &retry.ExitRetry{Err: err}
					case err == develop.GetAssetsInfoErrors.ErrUnauthorized:
						clientutils.GetNewCookie(ctx, r, "cookie expired")
					case strings.Contains(err.Error(), "429"):
						pause.extend(time.Duration(try) * assetsInfoRateLimitWait)
					default:
						switch err.(type) {
						case *net.OpError, *net.DNSError:
							queue.Limiter.Decrement()
						}
					}

					return develop.GetAssetsInfoResponse{}, &retry.ContinueRetry{Err: err}
				},
			)
			if err == nil {
				cacheAssetsInfo(ids, response)
			}
			return response, err
		}
	}

	ids := r.IDs

	chunkAmount := (len(ids) + AssetsInfoChunkSize - 1) / AssetsInfoChunkSize
	tasks := make([]chan AssetsInfoResult, 0, chunkAmount)
	for start := 0; start < len(ids); start += AssetsInfoChunkSize {
		idChunk := ids[start:min(start+AssetsInfoChunkSize, len(ids))]

		if cached, ok := cachedAssetsInfo(idChunk); ok {
			task := make(chan AssetsInfoResult, 1)
			task <- AssetsInfoResult{Result: cached}
			tasks = append(tasks, task)
			continue
		}
		tasks = append(tasks, queue.QueueTask(newAssetsInfoHandler(idChunk)))
	}

	return tasks
}
