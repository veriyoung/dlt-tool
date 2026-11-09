// Command dlt is a Super Lotto (大乐透) history analyzer and constrained ticket
// picker. It never predicts the next draw: draws are independent, and every
// legal combination has the same probability.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/veriyoung/dlt-tool/internal/analyze"
	"github.com/veriyoung/dlt-tool/internal/draw"
	"github.com/veriyoung/dlt-tool/internal/pick"
)

const usage = `dlt - 大乐透历史统计与选号工具

用法:
  dlt import -file tickets.csv [-data draws.json]
  dlt stats  [-data draws.json] [-json]
  dlt pick   [-data draws.json] [-n 5] [条件...] [-json]

子命令:
  import  导入开奖数据。每行格式 issue,date,front,back
          例如: 24001,2024-01-01,5 3 1 4 2,9 3
  stats   输出历史统计（频次、冷热、遗漏、和值、奇偶、连号）
  pick    生成满足约束的号码

pick 条件:
  -odd-even 3:2        前区奇偶比
  -sum 80:110          前区和值区间
  -zones 2:2:1         三区个数 1-12/13-24/25-35
  -max-consecutive 1   前区连号对数上限（0 表示不允许连号）
  -include 7,15        前区必含号码
  -exclude 1,2         前区排除号码
  -avoid-recent 10     排除最近 10 期出现过的前区号码
  -seed 42             固定随机种子以便复现
  -hot-bias 1.0        按历史频次加权（仅影响分布偏好，不改变中奖概率）
  -max-shared 2        任意两注前区最多共享号码数
  -no-duplicates       禁止生成完全相同的注

说明:
  本工具只做历史统计与约束随机采样。大乐透每期开奖相互独立，
  一等奖概率固定为 1/21425712，历史数据不构成对下一期的预测。
`

// ticketPrice is the face value of one Super Lotto ticket in CNY.
const ticketPrice = 2.0

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "import":
		err = runImport(os.Args[2:])
	case "stats":
		err = runStats(os.Args[2:])
	case "pick":
		err = runPick(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func runImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	file := fs.String("file", "", "CSV file with issue,date,front,back rows")
	dataPath := fs.String("data", "data/draws.json", "dataset path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return fmt.Errorf("import requires -file")
	}
	raw, err := os.ReadFile(*file)
	if err != nil {
		return fmt.Errorf("read %s: %w", *file, err)
	}
	draws, err := draw.ParseLines(string(raw))
	if err != nil {
		return err
	}
	if len(draws) == 0 {
		return fmt.Errorf("no rows found in %s", *file)
	}
	store, err := draw.Load(*dataPath)
	if err != nil {
		return err
	}
	before := len(store.Draws)
	for _, d := range draws {
		if err := store.Upsert(d); err != nil {
			return err
		}
	}
	if err := store.Save(*dataPath); err != nil {
		return err
	}
	fmt.Printf("imported %d rows, dataset %d -> %d draws, saved to %s\n", len(draws), before, len(store.Draws), *dataPath)
	return nil
}

func runStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	dataPath := fs.String("data", "data/draws.json", "dataset path")
	asJSON := fs.Bool("json", false, "emit JSON instead of text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := draw.Load(*dataPath)
	if err != nil {
		return err
	}
	if len(store.Draws) == 0 {
		return fmt.Errorf("dataset %s is empty; run the import subcommand first", *dataPath)
	}
	report := analyze.Build(store.Draws)
	if *asJSON {
		return writeJSON(report)
	}
	printReport(report)
	return nil
}

func runPick(args []string) error {
	fs := flag.NewFlagSet("pick", flag.ExitOnError)
	dataPath := fs.String("data", "data/draws.json", "dataset path")
	count := fs.Int("n", 5, "number of tickets")
	oddEven := fs.String("odd-even", "", "front odd:even split, e.g. 3:2")
	sumRange := fs.String("sum", "", "front sum range, e.g. 80:110")
	zones := fs.String("zones", "", "front zone counts 1-12/13-24/25-35, e.g. 2:2:1")
	maxConsecutive := fs.Int("max-consecutive", -1, "max adjacent pairs in front area, -1 disables")
	include := fs.String("include", "", "comma separated front numbers to require")
	exclude := fs.String("exclude", "", "comma separated front numbers to avoid")
	avoidRecent := fs.Int("avoid-recent", 0, "exclude front numbers drawn in the last N draws")
	seed := fs.Int64("seed", 0, "random seed, 0 uses current time")
	hotBias := fs.Float64("hot-bias", 0, "weight front numbers by historical frequency")
	maxShared := fs.Int("max-shared", 0, "max front numbers shared by any two tickets, 0 disables")
	noDuplicates := fs.Bool("no-duplicates", true, "reject tickets identical to an earlier ticket")
	asJSON := fs.Bool("json", false, "emit JSON instead of text")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *count <= 0 {
		return fmt.Errorf("-n must be positive, got %d", *count)
	}

	store, err := draw.Load(*dataPath)
	if err != nil {
		return err
	}

	c := pick.Constraint{}
	if *oddEven != "" {
		parts, err := parseInts(*oddEven, 2)
		if err != nil {
			return fmt.Errorf("-odd-even: %w", err)
		}
		c.OddEven = [2]int{parts[0], parts[1]}
	}
	if *sumRange != "" {
		parts, err := parseInts(*sumRange, 2)
		if err != nil {
			return fmt.Errorf("-sum: %w", err)
		}
		c.SumRange = [2]int{parts[0], parts[1]}
	}
	if *zones != "" {
		parts, err := parseInts(*zones, 3)
		if err != nil {
			return fmt.Errorf("-zones: %w", err)
		}
		c.Zones = [3]int{parts[0], parts[1], parts[2]}
	}
	if *maxConsecutive >= 0 {
		v := *maxConsecutive
		c.MaxConsecutive = &v
	}
	if *include != "" {
		nums, err := parseInts(*include, -1)
		if err != nil {
			return fmt.Errorf("-include: %w", err)
		}
		c.Include = nums
	}
	if *exclude != "" {
		nums, err := parseInts(*exclude, -1)
		if err != nil {
			return fmt.Errorf("-exclude: %w", err)
		}
		c.Exclude = nums
	}
	if *avoidRecent > 0 {
		if len(store.Draws) == 0 {
			return fmt.Errorf("-avoid-recent needs history, but %s is empty", *dataPath)
		}
		recent := pick.RecentNumbers(store.Draws, *avoidRecent)
		merged := append(append([]int(nil), c.Exclude...), recent...)
		c.Exclude = dedupe(merged)
	}

	useSeed := *seed
	if useSeed == 0 {
		useSeed = time.Now().UnixNano()
	}
	rng := rand.New(rand.NewSource(useSeed))

	counts := make(pick.HistoricalCounts)
	if *hotBias > 0 {
		for _, d := range store.Draws {
			for _, n := range d.Front {
				counts[n]++
			}
		}
	}

	tickets, err := pick.Recommend(rng, c, *count, pick.Options{
		Seed:                 useSeed,
		AvoidExactDuplicates: *noDuplicates,
		MaxSharedFront:       *maxShared,
		HotBias:              *hotBias,
	}, counts)
	if err != nil {
		return err
	}

	if *asJSON {
		return writeJSON(struct {
			Seed         int64         `json:"seed"`
			Tickets      []pick.Ticket `json:"tickets"`
			TotalCostCNY float64       `json:"total_cost_cny"`
			JackpotOdds  float64       `json:"jackpot_odds"`
			Disclaimer   string        `json:"disclaimer"`
		}{
			Seed:         useSeed,
			Tickets:      tickets,
			TotalCostCNY: float64(len(tickets)) * ticketPrice,
			JackpotOdds:  analyze.JackpotOdds,
			Disclaimer:   "以上为约束随机采样结果。每注中头奖概率固定为 1/21425712，历史数据不改变该概率。",
		})
	}

	fmt.Printf("随机种子: %d\n", useSeed)
	fmt.Printf("历史数据: %d 期\n\n", len(store.Draws))
	for i, tk := range tickets {
		fmt.Printf("%2d. 前区 %s  后区 %s\n", i+1, formatNums(tk.Front), formatNums(tk.Back))
	}
	fmt.Printf("\n共 %d 注，合计 %.0f 元\n", len(tickets), float64(len(tickets))*ticketPrice)
	fmt.Println("约束说明:", tickets[0].Reason)
	fmt.Println("提示: 每注中头奖概率固定为 1/21425712，历史数据不改变该概率。")
	return nil
}

func printReport(r analyze.Report) {
	fmt.Printf("样本期数: %d\n", r.Draws)
	fmt.Println()
	printArea("前区 1-35", r.Front)
	fmt.Println()
	printArea("后区 1-12", r.Back)
	fmt.Println()
	fmt.Println(r.Disclaimer)
}

func printArea(title string, a analyze.AreaReport) {
	fmt.Printf("== %s ==\n", title)
	fmt.Printf("和值: 最小 %d, 最大 %d, 均值 %.1f, 标准差 %.1f\n", a.SumMin, a.SumMax, a.SumMean, a.SumStdDev)
	fmt.Printf("连号对数累计: %d\n", a.Consecutive)
	fmt.Printf("出现最多: %s\n", formatStats(a.Hottest))
	fmt.Printf("出现最少: %s\n", formatStats(a.Coldest))
	keys := make([]string, 0, len(a.OddEven))
	for k := range a.OddEven {
		keys = append(keys, k)
	}
	sortStrings(keys)
	fmt.Print("奇偶分布:")
	for _, k := range keys {
		fmt.Printf(" %s×%d", k, a.OddEven[k])
	}
	fmt.Println()
}

func formatStats(stats []analyze.NumberStat) string {
	parts := make([]string, 0, len(stats))
	for _, s := range stats {
		parts = append(parts, fmt.Sprintf("%d(%d次)", s.Number, s.Count))
	}
	return strings.Join(parts, " ")
}

func formatNums(nums []int) string {
	parts := make([]string, 0, len(nums))
	for _, n := range nums {
		parts = append(parts, fmt.Sprintf("%02d", n))
	}
	return strings.Join(parts, " ")
}

func parseInts(text string, want int) ([]int, error) {
	fields := strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == ':' || r == ' ' })
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", f)
		}
		out = append(out, n)
	}
	if want >= 0 && len(out) != want {
		return nil, fmt.Errorf("expected %d numbers, got %d", want, len(out))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no numbers provided")
	}
	return out, nil
}

func dedupe(nums []int) []int {
	seen := make(map[int]struct{}, len(nums))
	out := make([]int, 0, len(nums))
	for _, n := range nums {
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	sortInts(out)
	return out
}

func sortInts(nums []int) {
	for i := 1; i < len(nums); i++ {
		for j := i; j > 0 && nums[j] < nums[j-1]; j-- {
			nums[j], nums[j-1] = nums[j-1], nums[j]
		}
	}
}

func sortStrings(items []string) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j] < items[j-1]; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}

func writeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
