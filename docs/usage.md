# 使用说明

大乐透（超级大乐透）历史统计与约束随机选号工具的完整使用指南。

- 前区：1-35 选 5
- 后区：1-12 选 2
- 一等奖概率：`1 / (C(35,5) × C(12,2)) = 1 / 21425712`

本工具**不做预测**。每期开奖独立随机，历史统计不改变中奖概率。

## 目录

- [安装](#安装)
- [数据集](#数据集)
- [命令总览](#命令总览)
- [fetch：抓取开奖历史](#fetch抓取开奖历史)
- [import：导入 CSV](#import导入-csv)
- [stats：历史统计](#stats历史统计)
- [pick：约束选号](#pick约束选号)
- [约束参数详解](#约束参数详解)
- [常见用法](#常见用法)
- [退出码与错误](#退出码与错误)
- [常见问题](#常见问题)

## 安装

需要 Go 1.25 或更高版本。

```bash
git clone https://github.com/veriyoung/super-lotto-tool.git
cd super-lotto-tool
go build -o bin/dlt ./cmd/dlt
```

编译后二进制在 `bin/dlt`。也可以直接 `go run ./cmd/dlt ...` 运行。

```bash
./bin/dlt -h
```

## 数据集

仓库自带 `data/draws.json`，包含 2007 年首期至最近一期的完整开奖历史（2945 期），克隆后即可直接使用。

所有子命令通过 `-data` 指定数据集路径，默认 `data/draws.json`：

```bash
./bin/dlt stats -data data/draws.json
```

数据按期号存储，同一期号重复写入会覆盖，因此抓取、导入都是安全的增量操作。几千期数据约 700 KB，纯 JSON 文件，不需要数据库。

数据字段：

| 字段 | 含义 |
| --- | --- |
| `issue` | 期号，纯数字字符串 |
| `date` | 开奖日期，`YYYY-MM-DD` |
| `front` | 前区 5 个号码，升序 |
| `back` | 后区 2 个号码，升序 |
| `pool_cny` | 开奖后奖池，人民币，可能缺失 |
| `sales_cny` | 当期销量，人民币，可能缺失 |

## 命令总览

```
dlt fetch  [-data draws.json] [-pages N] [-full]
dlt import -file tickets.csv [-data draws.json]
dlt stats  [-data draws.json] [-json]
dlt pick   [-data draws.json] [-n 5] [条件...] [-json]
```

## fetch：抓取开奖历史

从体彩官方公开接口 `webapi.sporttery.cn` 拉取真实开奖结果。

```bash
# 增量更新：只拉最新一页（100 期），日常更新用这个
./bin/dlt fetch -data data/draws.json

# 拉取全部历史（约 30 页，2-3 分钟），首次使用或数据缺失时用
./bin/dlt fetch -data data/draws.json -full

# 只拉前 N 页
./bin/dlt fetch -data data/draws.json -pages 3
```

参数：

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `-data` | `data/draws.json` | 数据集路径 |
| `-pages` | `1` | 拉取页数，每页 100 期 |
| `-full` | `false` | 拉取全部历史，忽略 `-pages` |

行为说明：

- 每页 100 条，页间隔 5 秒，遇 403/429/5xx 指数退避重试，最多重试 4 次。
- 该接口有 WAF：`pageSize` 为 30/50/200 会被拒，只有 100 能通过；请求过快会封 IP。请保持默认参数。
- 抓取中途失败时，已获取的数据会先落盘再报错，下次运行接着补全，不会白跑。

## import：导入 CSV

从本地 CSV 导入，每行一期。

```bash
./bin/dlt import -file real-draws.csv -data data/draws.json
```

CSV 格式：

```
issue,date,front,back
24001,2024-01-01,5 9 12 20 33,4 7
24002,2024-01-03,2 14 18 27 35,1 11
```

- 以 `#` 开头的行和 `issue` 表头行会被跳过。
- 前后区可合并为第 3 列 7 个号码，程序自动按前 5 后 2 拆分。
- 号码分隔符支持空格、`-`、`|`。
- 也支持 `issue,date,front` 三列（第 3 列含全部 7 个号码）。

## stats：历史统计

```bash
./bin/dlt stats -data data/draws.json
./bin/dlt stats -data data/draws.json -json
```

输出内容：

- 样本期数
- 前区/后区的和值最小/最大/均值/标准差
- 出现次数最多的 5 个号码
- 出现次数最少的 5 个号码
- 奇偶分布直方图
- 连号对数累计

`-json` 输出完整结构，含每个号码的出现次数、频率、理论期望值、偏差和遗漏期数，便于二次分析。

`-json` 输出示例（节选）：

```json
{
  "draws": 2945,
  "front": {
    "total": 2945,
    "numbers": [
      {"number": 1, "count": 432, "frequency": 0.1467, "expected": 420.71, "deviation": 11.29, "missed": 7}
    ],
    "sum_min": 27,
    "sum_max": 163,
    "sum_mean": 91.7,
    "sum_std_dev": 22.4,
    "odd_even": {"3:2": 1072},
    "consecutive_pairs": 1786
  }
}
```

## pick：约束选号

在所有满足条件的合法组合中等概率抽样。约束改变号码长什么样，不改变中奖概率。

```bash
./bin/dlt pick -data data/draws.json -n 5
```

参数：

| 参数 | 默认 | 说明 |
| --- | --- | --- |
| `-n` | `5` | 注数 |
| `-odd-even` | 无 | 前区奇偶比，如 `3:2` |
| `-sum` | 无 | 前区和值区间，如 `80:110` |
| `-zones` | 无 | 三区个数，顺序为 1-12 / 13-24 / 25-35，如 `2:2:1` |
| `-max-consecutive` | 无 | 前区连号对数上限，`0` 表示不允许连号 |
| `-include` | 无 | 前区必含号码，逗号分隔 |
| `-exclude` | 无 | 前区排除号码，逗号分隔 |
| `-avoid-recent` | `0` | 排除最近 N 期出现过的前区号码 |
| `-hot-bias` | `0` | 按历史频次加权，仅影响分布偏好，不改变概率 |
| `-max-shared` | `0` | 任意两注前区最多共享号码数，`0` 不限制 |
| `-no-duplicates` | `true` | 禁止生成完全相同的注 |
| `-seed` | `0` | 随机种子，`0` 用当前时间 |
| `-json` | `false` | 输出 JSON |

## 约束参数详解

### `-odd-even 3:2`

前区 5 个号码中奇数与偶数的个数比。`3:2` 表示 3 奇 2 偶，`2:3` 表示 2 奇 3 偶。

### `-sum 80:110`

前区 5 个号码之和的闭区间，含端点。前区 5 个号码的和值理论范围是 15（1+2+3+4+5）到 165（31+32+33+34+35）。

### `-zones 2:2:1`

三区分布：1-12 取几个、13-24 取几个、25-35 取几个，三者之和必须为 5。

### `-max-consecutive 1`

前区相邻号码对的数量上限。例如 `5 6 12 20 30` 有 1 对连号（5-6）。

- `-max-consecutive 0`：不允许任何连号
- `-max-consecutive 1`：最多 1 对
- 省略该参数：不限制

### `-include 7,15` / `-exclude 1,2`

强制包含或排除指定前区号码。两者冲突（同一号码既 include 又 exclude）时返回错误。

### `-avoid-recent 10`

排除最近 10 期出现过的所有前区号码。需要数据集非空。与 `-exclude` 自动合并。

### `-hot-bias 1.0`

按历史出现频次给号码加权，值越大越偏向热号。这是**分布偏好**，不提高中奖概率。`0` 表示完全均匀。

### `-max-shared 2`

限制任意两注之间前区共享号码的最大数量，避免多注高度雷同。`0` 表示不限制。

### `-seed 42`

固定随机种子，同一约束加同一种子必定生成同一组号码，便于复现。`0` 表示用当前纳秒时间。

## 常见用法

```bash
# 完全均匀随机，5 注
./bin/dlt pick -n 5

# 奇偶均衡、和值居中，3 注
./bin/dlt pick -n 3 -odd-even 3:2 -sum 80:110

# 三区均衡、最多一组连号
./bin/dlt pick -n 5 -zones 2:2:1 -max-consecutive 1

# 必含 7、15，排除最近 10 期出现过的号码
./bin/dlt pick -n 5 -include 7,15 -avoid-recent 10

# 10 注，任意两注前区最多共享 2 个号码
./bin/dlt pick -n 10 -max-shared 2

# 结果可复现
./bin/dlt pick -n 5 -seed 42

# 输出 JSON 供程序处理
./bin/dlt pick -n 5 -json
```

`pick -json` 输出结构：

```json
{
  "seed": 42,
  "tickets": [
    {"front": [7, 11, 28, 32, 35], "back": [9, 11], "reason": "..."}
  ],
  "total_cost_cny": 10,
  "jackpot_odds": 4.666e-08,
  "disclaimer": "以上为约束随机采样结果。..."
}
```

## 退出码与错误

| 退出码 | 含义 |
| --- | --- |
| `0` | 成功 |
| `1` | 运行错误（数据为空、约束不可满足、抓取失败等） |
| `2` | 用法错误（未知子命令、无参数） |

约束过严导致在 20 万次拒绝采样上限内无法产出足够注数时，程序返回 `ErrNoValidCombination` 错误，不会悄悄放宽条件。遇到这种情况请放宽约束或减少注数。

## 常见问题

**Q：这个工具能预测下期号码吗？**

不能。大乐透每期独立随机，历史数据对下一期没有预测力。本工具只做历史统计和约束随机采样。

**Q：为什么 `stats` 显示某号码"热"，下期更可能出吗？**

不会。长期看每个号码出现频率趋近期望值，短期偏离是随机波动。热号冷号都是描述过去，不预示未来。

**Q：`fetch` 报 403 怎么办？**

接口有 WAF 限流。等待几分钟后重试，并确认没有缩短 `Pause` 或并发请求。已抓到的数据会自动保存，续跑即可。

**Q：`-hot-bias` 能提高中奖率吗？**

不能。它只改变生成号码的分布偏好，每注中奖概率始终是 1/21425712。

**Q：数据需要联网吗？**

仓库已内置完整历史数据，离线可用。只有 `fetch` 需要联网，`import`、`stats`、`pick` 完全离线。
