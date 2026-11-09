// Package source fetches Super Lotto draw history from the public China Sports
// Lottery endpoint and maps it into draw.Draw records.
//
// The endpoint is a public web API used by the official site. Callers should
// keep request rates modest and cache results locally rather than refetching
// the whole history on every run.
package source

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/veriyoung/dlt-tool/internal/draw"
)

// GameNoSuperLotto is the game identifier for 超级大乐透.
const GameNoSuperLotto = "85"

// DefaultEndpoint is the public history API.
const DefaultEndpoint = "https://webapi.sporttery.cn/gateway/lottery/getHistoryPageListV1.qry"

// userAgent mimics a browser because the endpoint rejects default Go clients.
const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// Client fetches draw pages.
type Client struct {
	Endpoint string
	PageSize int
	HTTP     *http.Client
	// Pause is the delay inserted between page requests.
	Pause time.Duration
	// Retries is the number of extra attempts for retryable failures (403/429/5xx).
	Retries int
	// RetryBackoff is the base delay before the first retry; it doubles each try.
	RetryBackoff time.Duration
}

// NewClient returns a client with sensible defaults.
//
// PageSize 100 and a multi-second pause are deliberate: the public endpoint
// sits behind a WAF that blocks pageSize 30/50/200 and bans bursty clients.
func NewClient() *Client {
	return &Client{
		Endpoint:     DefaultEndpoint,
		PageSize:     100,
		HTTP:         &http.Client{Timeout: 25 * time.Second},
		Pause:        5 * time.Second,
		Retries:      4,
		RetryBackoff: 15 * time.Second,
	}
}

type apiResponse struct {
	Success bool   `json:"success"`
	Code    string `json:"errorCode"`
	Message string `json:"errorMessage"`
	Value   struct {
		PageNo   int `json:"pageNo"`
		PageSize int `json:"pageSize"`
		Pages    int `json:"pages"`
		Total    int `json:"total"`
		List     []struct {
			Num    string `json:"lotteryDrawNum"`
			Result string `json:"lotteryDrawResult"`
			DrawAt string `json:"lotteryDrawTime"`
			Pool   string `json:"poolBalanceAfterdraw"`
			Sales  string `json:"totalSaleAmount"`
		} `json:"list"`
	} `json:"value"`
}

// Page fetches one page of results, 1-indexed. Retryable failures (403, 429,
// 5xx and transient network errors) are retried with exponential backoff.
func (c *Client) Page(ctx context.Context, pageNo int) ([]draw.Draw, int, error) {
	if pageNo < 1 {
		return nil, 0, fmt.Errorf("page number must be >= 1, got %d", pageNo)
	}
	backoff := c.RetryBackoff
	if backoff <= 0 {
		backoff = time.Second
	}
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(backoff):
			}
			backoff *= 2
		}
		draws, pages, err := c.fetchOnce(ctx, pageNo)
		if err == nil {
			return draws, pages, nil
		}
		lastErr = err
		if !isRetryable(err) {
			return nil, 0, err
		}
	}
	return nil, 0, fmt.Errorf("page %d failed after %d attempts: %w", pageNo, c.Retries+1, lastErr)
}

// retryableError marks a failure worth retrying.
type retryableError struct{ err error }

func (e retryableError) Error() string { return e.err.Error() }
func (e retryableError) Unwrap() error { return e.err }

func isRetryable(err error) bool {
	var re retryableError
	return errors.As(err, &re)
}

func (c *Client) fetchOnce(ctx context.Context, pageNo int) ([]draw.Draw, int, error) {
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	url := fmt.Sprintf("%s?gameNo=%s&provinceId=0&pageSize=%d&isVerify=1&pageNo=%d",
		endpoint, GameNoSuperLotto, c.pageSize(), pageNo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", "https://static.sporttery.cn/")

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, retryableError{fmt.Errorf("fetch page %d: %w", pageNo, err)}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("fetch page %d: status %d", pageNo, resp.StatusCode)
		if resp.StatusCode == http.StatusForbidden ||
			resp.StatusCode == http.StatusTooManyRequests ||
			resp.StatusCode >= 500 {
			return nil, 0, retryableError{err}
		}
		return nil, 0, err
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, 0, retryableError{fmt.Errorf("read page %d: %w", pageNo, err)}
	}
	var parsed apiResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, 0, fmt.Errorf("parse page %d: %w (body starts with %q)", pageNo, err, truncate(raw, 80))
	}
	if !parsed.Success {
		return nil, 0, fmt.Errorf("api rejected page %d: code=%s message=%s", pageNo, parsed.Code, parsed.Message)
	}

	out := make([]draw.Draw, 0, len(parsed.Value.List))
	for _, row := range parsed.Value.List {
		d, err := convert(row.Num, row.Result, row.DrawAt, row.Pool, row.Sales)
		if err != nil {
			return nil, 0, fmt.Errorf("page %d issue %s: %w", pageNo, row.Num, err)
		}
		out = append(out, d)
	}
	return out, parsed.Value.Pages, nil
}

// FetchAll downloads every page. onPage, when non-nil, receives progress after
// each page.
func (c *Client) FetchAll(ctx context.Context, onPage func(page, pages, got int)) ([]draw.Draw, error) {
	first, pages, err := c.Page(ctx, 1)
	if err != nil {
		return nil, err
	}
	all := append([]draw.Draw(nil), first...)
	if onPage != nil {
		onPage(1, pages, len(all))
	}
	for page := 2; page <= pages; page++ {
		if err := ctx.Err(); err != nil {
			return all, err
		}
		if c.Pause > 0 {
			select {
			case <-ctx.Done():
				return all, ctx.Err()
			case <-time.After(c.Pause):
			}
		}
		batch, _, err := c.Page(ctx, page)
		if err != nil {
			return all, err
		}
		all = append(all, batch...)
		if onPage != nil {
			onPage(page, pages, len(all))
		}
	}
	return all, nil
}

func (c *Client) pageSize() int {
	if c.PageSize <= 0 {
		return 100
	}
	if c.PageSize > 100 {
		return 100
	}
	return c.PageSize
}

func convert(num, result, date, pool, sales string) (draw.Draw, error) {
	nums, err := parseResult(result)
	if err != nil {
		return draw.Draw{}, err
	}
	if len(nums) != draw.FrontPick+draw.BackPick {
		return draw.Draw{}, fmt.Errorf("result %q has %d numbers, want %d", result, len(nums), draw.FrontPick+draw.BackPick)
	}
	d := draw.Draw{
		Issue:    strings.TrimSpace(num),
		Date:     strings.TrimSpace(date),
		Front:    nums[:draw.FrontPick],
		Back:     nums[draw.FrontPick:],
		PoolCNY:  parseMoney(pool),
		SalesCNY: parseMoney(sales),
	}.Normalize()
	if err := d.Validate(); err != nil {
		return draw.Draw{}, err
	}
	return d, nil
}

func parseResult(text string) ([]int, error) {
	fields := strings.Fields(text)
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil, fmt.Errorf("result %q contains non-numeric token %q", text, f)
		}
		out = append(out, n)
	}
	return out, nil
}

func parseMoney(text string) float64 {
	clean := strings.NewReplacer(",", "", " ", "", "¥", "", "元", "").Replace(strings.TrimSpace(text))
	if clean == "" {
		return 0
	}
	v, err := strconv.ParseFloat(clean, 64)
	if err != nil {
		return 0
	}
	return v
}

func truncate(raw []byte, n int) string {
	if len(raw) <= n {
		return string(raw)
	}
	return string(raw[:n]) + "..."
}
