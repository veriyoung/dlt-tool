package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func pageJSON(t *testing.T, page, pages int, rows ...[3]string) []byte {
	t.Helper()
	list := make([]map[string]string, 0, len(rows))
	for _, r := range rows {
		list = append(list, map[string]string{
			"lotteryDrawNum":       r[0],
			"lotteryDrawResult":    r[1],
			"lotteryDrawTime":      r[2],
			"poolBalanceAfterdraw": "762,911,586.98",
			"totalSaleAmount":      "332,972,216",
		})
	}
	payload := map[string]any{
		"success":      true,
		"errorCode":    "0",
		"errorMessage": "处理成功",
		"value": map[string]any{
			"pageNo": page, "pageSize": 30, "pages": pages, "total": len(list),
			"list": list,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPageParsesDraws(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("gameNo"); got != GameNoSuperLotto {
			t.Errorf("gameNo = %q", got)
		}
		if got := r.URL.Query().Get("pageNo"); got != "1" {
			t.Errorf("pageNo = %q", got)
		}
		if ua := r.Header.Get("User-Agent"); ua == "" {
			t.Error("missing user agent")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(pageJSON(t, 1, 2,
			[3]string{"26127", "05 06 17 18 27 03 12", "2026-11-07"},
			[3]string{"26126", "05 11 17 25 34 04 06", "2026-11-04"},
		))
	}))
	defer srv.Close()

	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}
	draws, pages, err := c.Page(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if pages != 2 {
		t.Fatalf("pages = %d", pages)
	}
	if len(draws) != 2 {
		t.Fatalf("draws = %d", len(draws))
	}
	first := draws[0]
	if first.Issue != "26127" || first.Date != "2026-11-07" {
		t.Fatalf("unexpected draw: %+v", first)
	}
	if got := first.Front; len(got) != 5 || got[0] != 5 || got[4] != 27 {
		t.Fatalf("front = %v", got)
	}
	if got := first.Back; len(got) != 2 || got[0] != 3 || got[1] != 12 {
		t.Fatalf("back = %v", got)
	}
	if first.PoolCNY != 762911586.98 {
		t.Fatalf("pool = %v", first.PoolCNY)
	}
	if first.SalesCNY != 332972216 {
		t.Fatalf("sales = %v", first.SalesCNY)
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestPageRejectsNonJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<!doctype html><html>blocked</html>"))
	}))
	defer srv.Close()

	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}
	if _, _, err := c.Page(context.Background(), 1); err == nil {
		t.Fatal("expected parse error for HTML body")
	}
}

func TestPageRejectsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"success":false,"errorCode":"1001","errorMessage":"bad request","value":{}}`))
	}))
	defer srv.Close()

	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}
	if _, _, err := c.Page(context.Background(), 1); err == nil {
		t.Fatal("expected api error")
	}
}

func TestPageRejectsBadNumbers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(pageJSON(t, 1, 1, [3]string{"1", "01 02 03 04 36 01 02", "2026-01-01"}))
	}))
	defer srv.Close()

	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}
	if _, _, err := c.Page(context.Background(), 1); err == nil {
		t.Fatal("expected range validation error")
	}
}

func TestFetchAllPages(t *testing.T) {
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("pageNo")
		requested = append(requested, page)
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case "1":
			_, _ = w.Write(pageJSON(t, 1, 2, [3]string{"3", "01 02 03 04 05 01 02", "2026-01-03"}))
		case "2":
			_, _ = w.Write(pageJSON(t, 2, 2, [3]string{"4", "06 07 08 09 10 03 04", "2026-01-04"}))
		default:
			t.Errorf("unexpected page %q", page)
		}
	}))
	defer srv.Close()

	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}
	var progress []int
	draws, err := c.FetchAll(context.Background(), func(page, pages, got int) {
		progress = append(progress, got)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(draws) != 2 {
		t.Fatalf("draws = %d", len(draws))
	}
	if len(progress) != 2 || progress[0] != 1 || progress[1] != 2 {
		t.Fatalf("progress = %v", progress)
	}
	if len(requested) != 2 || requested[0] != "1" || requested[1] != "2" {
		t.Fatalf("requested pages = %v", requested)
	}
}

func TestFetchAllStopsOnContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(pageJSON(t, 1, 5, [3]string{"1", "01 02 03 04 05 01 02", "2026-01-01"}))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{Endpoint: srv.URL, HTTP: srv.Client()}
	_, err := c.FetchAll(ctx, func(page, pages, got int) {
		if page == 1 {
			cancel()
		}
	})
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestParseMoney(t *testing.T) {
	cases := map[string]float64{
		"762,911,586.98": 762911586.98,
		"332972216":      332972216,
		"":               0,
		"¥1,000 元":       1000,
		"garbage":        0,
	}
	for in, want := range cases {
		if got := parseMoney(in); got != want {
			t.Fatalf("parseMoney(%q) = %v want %v", in, got, want)
		}
	}
}

func TestPageSizeBounds(t *testing.T) {
	if (&Client{}).pageSize() != 100 {
		t.Fatal("default page size should be 100")
	}
	if (&Client{PageSize: 500}).pageSize() != 100 {
		t.Fatal("page size should clamp to 100")
	}
}

func TestConvertRejectsWrongArity(t *testing.T) {
	if _, err := convert("1", "01 02 03 04 05 06", "2026-01-01", "", ""); err == nil {
		t.Fatal("expected arity error for 6 numbers")
	}
}

func TestConvertRejectsOutOfRange(t *testing.T) {
	if _, err := convert("1", "01 02 03 04 36 01 02", "2026-01-01", "", ""); err == nil {
		t.Fatal("expected range error for front number 36")
	}
}

func TestPageRetriesTransientFailures(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(pageJSON(t, 1, 1, [3]string{"1", "01 02 03 04 05 01 02", "2026-01-01"}))
	}))
	defer srv.Close()

	c := &Client{Endpoint: srv.URL, HTTP: srv.Client(), Retries: 4, RetryBackoff: time.Millisecond}
	draws, _, err := c.Page(context.Background(), 1)
	if err != nil {
		t.Fatalf("expected retry to succeed: %v", err)
	}
	if len(draws) != 1 {
		t.Fatalf("draws = %d", len(draws))
	}
	if hits != 3 {
		t.Fatalf("hits = %d, want 3", hits)
	}
}

func TestPageGivesUpAfterRetries(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := &Client{Endpoint: srv.URL, HTTP: srv.Client(), Retries: 2, RetryBackoff: time.Millisecond}
	if _, _, err := c.Page(context.Background(), 1); err == nil {
		t.Fatal("expected failure after retries")
	}
	if hits != 3 {
		t.Fatalf("hits = %d, want 3 (1 initial + 2 retries)", hits)
	}
}

func TestPageDoesNotRetryNonTransient(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := &Client{Endpoint: srv.URL, HTTP: srv.Client(), Retries: 3, RetryBackoff: time.Millisecond}
	if _, _, err := c.Page(context.Background(), 1); err == nil {
		t.Fatal("expected 400 failure")
	}
	if hits != 1 {
		t.Fatalf("hits = %d, want 1 (no retry for 400)", hits)
	}
}
