# 大乐透历史统计与选号工具

体彩大乐透（前区 1-35 选 5，后区 1-12 选 2）的历史统计与约束随机选号工具。

一条命令拉取真实开奖数据，做统计分析，按约束随机选号。

```bash
go build -o bin/dlt ./cmd/dlt
./bin/dlt fetch -data data/draws.json -full   # 拉取全部历史开奖
./bin/dlt stats -data data/draws.json         # 频次/冷热/遗漏/和值/奇偶
./bin/dlt pick  -data data/draws.json -n 5    # 约束随机选号
```

仓库已内置完整开奖历史（2945 期，2007 年首期至最近一期），克隆后可直接使用，无需先抓取。

完整使用说明见 [`docs/usage.md`](docs/usage.md)。

## 先说清楚边界

本工具**不做预测**。

大乐透每期开奖是独立随机事件，历史结果对下一期没有任何预测力。一等奖概率固定为：

```
1 / (C(35,5) × C(12,2)) = 1 / (324632 × 66) = 1 / 21425712
```

程序能做两件事：

1. **统计**：频次、冷热、遗漏、和值分布、奇偶比、连号。这些描述的是过去。
2. **约束选号**：在所有满足你设定条件的合法组合里等概率抽样。约束改变的是号码长什么样，不改变中奖概率。

任何声称能提高中奖率的程序都在骗人。

## 构建

```bash
go build -o bin/dlt ./cmd/dlt
```

## 数据

仓库已附带 `data/draws.json`，内含从 2007 年首期到最近一期的完整开奖历史（2945 期），克隆后即可直接使用，无需先抓取。

`data/example.csv` 是 CSV 导入格式的示例（非真实数据）。

### 直接使用仓库内置数据

```bash
git clone https://github.com/veriyoung/super-lotto-tool.git
cd super-lotto-tool
go build -o bin/dlt ./cmd/dlt
./bin/dlt stats -data data/draws.json
```

### 更新数据

仓库里的数据是某个时间点的快照。想拉到最新一期，运行 `fetch` 做增量更新：

```bash
# 增量更新：只拉最新一页（100 期）
./bin/dlt fetch -data data/draws.json

# 首次使用或数据缺失，拉取全部历史（约 2900+ 期）
./bin/dlt fetch -data data/draws.json -full

# 只拉前 N 页
./bin/dlt fetch -data data/draws.json -pages 3
```

数据来源是体彩官方站点使用的公开历史开奖接口 `webapi.sporttery.cn`。抓取写入 `data/draws.json`，与 `import` 共用同一个数据集，按期号增量覆盖。

抓取参数针对该接口的反爬策略做了适配：每页 100 条、页间间隔 5 秒、遇 403/429/5xx 指数退避重试。全量约 30 页，耗时 2-3 分钟。如果抓取中途被拦截，已经拿到的数据会立即落盘，下次运行会接着补全，不会白跑。

请保持默认的间隔参数。缩短间隔或并发抓取会触发 WAF 封禁，且给对手方站点造成不必要的压力。

### 手动导入

也可以自己整理数据，每行一期：

```
issue,date,front,back
24001,2024-01-01,5 9 12 20 33,4 7
24002,2024-01-03,2 14 18 27 35,1 11
```

- `issue`：期号，纯数字
- `date`：开奖日期，`YYYY-MM-DD`
- `front`：5 个前区号码，空格分隔，1-35
- `back`：2 个后区号码，空格分隔，1-12

前区和后区也可以合并成第 3 列 7 个号码，程序会自动拆分。

```bash
./bin/dlt import -file real-draws.csv -data data/draws.json
```

两种方式都是增量的，同一期号会覆盖。数据存成一个 JSON 文件，几千期也只有几百 KB，不需要数据库。

## 统计

```bash
./bin/dlt stats -data data/draws.json
./bin/dlt stats -data data/draws.json -json
```

输出内容：

- 前区与后区的和值最小/最大/均值/标准差
- 出现次数最多的 5 个号码
- 出现次数最少的 5 个号码（含从未出现的）
- 奇偶分布直方
- 连号对数累计

`-json` 输出完整结构，含每个号码的出现次数、频率、理论期望值、偏差和遗漏期数。

## 选号

```bash
# 3 注，奇偶比 3:2，和值 60-110
./bin/dlt pick -data data/draws.json -n 3 -odd-even 3:2 -sum 60:110

# 5 注，三区 2:2:1，最多 1 组连号，禁止重复
./bin/dlt pick -n 5 -zones 2:2:1 -max-consecutive 1

# 必含 7、15，排除最近 10 期出现过的号码
./bin/dlt pick -n 5 -include 7,15 -avoid-recent 10 -data data/draws.json

# 多注之间前区最多共享 2 个号码
./bin/dlt pick -n 10 -max-shared 2

# 固定种子，结果可复现
./bin/dlt pick -n 5 -seed 42
```

### 参数

| 参数 | 说明 |
| --- | --- |
| `-n` | 注数，默认 5 |
| `-odd-even` | 前区奇偶比，如 `3:2` |
| `-sum` | 前区和值区间，如 `80:110` |
| `-zones` | 三区个数，顺序为 1-12 / 13-24 / 25-35 |
| `-max-consecutive` | 前区连号对数上限，`0` 表示不允许连号，省略表示不限制 |
| `-include` | 前区必含号码，逗号分隔 |
| `-exclude` | 前区排除号码，逗号分隔 |
| `-avoid-recent` | 排除最近 N 期出现过的前区号码 |
| `-hot-bias` | 按历史频次加权，默认 0（均匀） |
| `-max-shared` | 任意两注前区最多共享号码数，默认 0（不限制） |
| `-no-duplicates` | 禁止完全相同的注，默认开启 |
| `-seed` | 随机种子，0 表示用当前时间 |
| `-json` | 输出 JSON |

### 采样方式

所有约束通过拒绝采样实现：随机生成组合，校验约束，不满足就重抽，默认上限 20 万次。约束过严导致无法在限制内产出足够注数时，程序返回错误而不是悄悄放宽条件。

`-hot-bias` 会按历史频次给号码加权。这**不会**提高中奖概率，只让输出更贴近你的偏好。统计页会同时显示历史次数和理论期望值，偏差一目了然。

`-max-shared` 改变的是多注之间的结构，避免几注号码高度雷同。单注中奖概率不变。

### 复现

输出里会打印随机种子。同一约束加同一种子必定生成同一组号码：

```bash
./bin/dlt pick -n 5 -seed 42
```

## 输出说明

文本输出示例：

```
随机种子: 42
历史数据: 5 期

 1. 前区 15 16 21 22 29  后区 07 10
 2. 前区 06 07 11 25 28  后区 05 11
 3. 前区 06 17 20 23 35  后区 06 09

共 3 注，合计 6 元
约束说明: 奇偶 3:2; 和值 60-110
提示: 每注中头奖概率固定为 1/21425712，历史数据不改变该概率。
```

每注面值 2 元，总花费按注数计算。

## 测试

```bash
go test ./...
go vet ./...
```

覆盖：号码规则校验、数据解析、统计正确性、约束采样、复现性、重复与共享控制、不可能的约束返回错误。

## 目录

```
cmd/dlt/            命令行入口
internal/draw/      开奖记录模型、校验、数据存取
internal/analyze/   统计与概率常量
internal/pick/      约束采样与推荐
internal/source/    官方历史开奖接口抓取
data/draws.json     内置开奖历史（2007 首期至今，2945 期）
data/example.csv    CSV 导入格式示例（非真实数据）
```
