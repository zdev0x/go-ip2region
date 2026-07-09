# go-ip2region

基于 [ip2region](https://github.com/lionsoul2014/ip2region) 的 IP 属地查询微服务，支持**单 IP 查询**与**批量查询**，采用官方并发安全的查询内核，设计简洁、高性能、易部署。

- 源码仓库：<https://github.com/zdev0x/go-ip2region>
- Go 模块：`github.com/zdev0x/go-ip2region`

## 特性

- **单查 / 批量**：`GET /api/v1/ip/query` 单 IP 查询；`POST /api/v1/ip/batch` 批量查询（并发执行、结果有序、单条失败不影响整体）。
- **高性能**：默认全内存缓存（`content`），查询无磁盘 IO，约 10μs 级；内置 searcher 池提供并发背压。
- **并发安全**：直接复用官方 `service.Ip2Region` 并发查询内核，无需自行管理锁。
- **可配置**：支持 YAML 配置文件 + 环境变量（环境变量优先级更高），缓存策略、并发度、批量上限、超时均可调。
- **零停机更新**：xdb 数据文件更新后，向进程发送 `SIGHUP` 即可热加载，加载失败自动保留旧数据。
- **运维友好**：结构化 JSON 日志、优雅关停、可选 PID 文件、内置健康检查。
- **轻依赖**：仅依赖 ip2region 官方 Go 绑定，Web 层使用标准库 `net/http`。

## 目录结构

```
.
├── main.go                 # 入口：装配、路由、信号处理、优雅关停
├── config/                 # 配置加载（YAML + 环境变量，含单测）
├── model/                  # 请求/响应 DTO 与 Region 结构
├── ip2region/              # 对官方查询内核的封装（单查 + 批量并行，含单测）
├── handler/                # HTTP 处理器（Health / Query / Batch）
├── config.yaml             # 默认配置文件
├── scripts/update-xdb.sh   # xdb 数据更新脚本（供 crontab 调用）
├── deploy/ip2region.service# systemd 单元文件
├── Dockerfile              # 多阶段构建镜像
└── .gitignore
```

## 快速开始

### 1. 获取 xdb 数据文件

服务不内置数据文件，需提供 `ip2region_v4.xdb` 与 `ip2region_v6.xdb`（IPv4 / IPv6 数据，缺一则对应版本被禁用）。
从官方仓库获取（LFS 二进制）：

```bash
# 方式一：使用内置更新脚本（推荐，支持定时更新）
IP2REGION_XDB_DIR=./data bash scripts/update-xdb.sh

# 方式二：手动下载
curl -fsSL https://raw.githubusercontent.com/lionsoul2014/ip2region/master/data/ip2region_v4.xdb -o data/ip2region_v4.xdb
curl -fsSL https://raw.githubusercontent.com/lionsoul2014/ip2region/master/data/ip2region_v6.xdb -o data/ip2region_v6.xdb
```

### 2. 构建与运行

```bash
go build -o go-ip2region .

# 通过配置文件运行（xdb 路径在 config.yaml 中配置）
./go-ip2region -config config.yaml

# 或通过环境变量覆盖（示例）
IP2REGION_V4_XDB=./data/ip2region_v4.xdb \
IP2REGION_V6_XDB=./data/ip2region_v6.xdb \
IP2REGION_PID_FILE=/run/ip2region.pid \
./go-ip2region
```

## 配置

加载优先级（由低到高）：**内置默认值 < `config.yaml` < 环境变量**。

`config.yaml` 示例：

```yaml
http_addr: ":8080"
read_timeout: "5s"
write_timeout: "10s"
v4_xdb_path: "/data/ip2region_v4.xdb"
v6_xdb_path: "/data/ip2region_v6.xdb"
cache_policy: "content"   # file | vectorindex | content
searchers: 0              # 0 表示按 GOMAXPROCS*2 自动计算
max_batch_size: 1000
pid_file: ""              # 非空时写入 PID，供更新脚本发送信号
```

环境变量（均可覆盖配置文件）：

| 环境变量 | 默认值 | 说明 |
|---|---|---|
| `IP2REGION_CONFIG` | `config.yaml` | 配置文件路径 |
| `IP2REGION_HTTP_ADDR` | `:8080` | 监听地址 |
| `IP2REGION_READ_TIMEOUT` | `5s` | 读超时 |
| `IP2REGION_WRITE_TIMEOUT` | `10s` | 写超时 |
| `IP2REGION_V4_XDB` | 空 | v4 xdb 路径（空则禁用 v4） |
| `IP2REGION_V6_XDB` | 空 | v6 xdb 路径（空则禁用 v6） |
| `IP2REGION_CACHE_POLICY` | `content` | `file` / `vectorindex` / `content` |
| `IP2REGION_SEARCHERS` | `GOMAXPROCS*2` | searcher 池大小 |
| `IP2REGION_MAX_BATCH` | `1000` | 批量查询单次最大 IP 数 |
| `IP2REGION_PID_FILE` | 空 | PID 文件路径 |

> 缓存策略说明：`content` 全内存（最快，内存≈xdb 大小）；`vectorindex` 仅缓存向量索引（约 512KB，略慢）；`file` 无缓存（每次读盘，最省内存）。

## API

统一错误响应：`{ "code": <int>, "message": "<string>" }`。

### 健康检查

```bash
GET /healthz
# -> {"status":"ok"}
```

### 单 IP 查询

```bash
GET /api/v1/ip/query?ip=223.5.5.5
```

响应（`Region`）：

```json
{
  "ip": "223.5.5.5",
  "country": "中国",
  "province": "浙江省",
  "city": "杭州市",
  "isp": "阿里",
  "code": "CN",
  "region": "中国|浙江省|杭州市|阿里|CN"
}
```

### 批量查询

```bash
POST /api/v1/ip/batch
Content-Type: application/json

{ "ips": ["223.5.5.5", "8.8.8.8", "not-an-ip"] }
```

响应：

```json
{
  "count": 3,
  "results": [
    { "ip": "223.5.5.5", "country": "中国", "province": "浙江省", "city": "杭州市", "isp": "阿里", "code": "CN", "region": "中国|浙江省|杭州市|阿里|CN" },
    { "ip": "8.8.8.8", "country": "United States", "province": "California", "city": "", "isp": "Google LLC", "code": "US", "region": "United States|California|0|Google LLC|US" },
    { "ip": "not-an-ip", "country": "", "province": "", "city": "", "isp": "", "code": "", "region": "", "error": "非法 IP 地址: not-an-ip" }
  ]
}
```

> 批量结果顺序与入参一致；单条非法/失败仅在该项的 `error` 字段体现，不影响其他项。

## 数据更新（定时任务）

`scripts/update-xdb.sh` 下载最新 xdb、校验后原子替换，并向运行中的服务发送 `SIGHUP` 触发热加载（零停机）。加入 crontab：

```cron
# 每日 03:17 更新
17 3 * * * /opt/ip2region/scripts/update-xdb.sh >> /var/log/ip2region-update.log 2>&1
```

脚本支持的覆盖环境变量：`IP2REGION_XDB_DIR`、`IP2REGION_XDB_BRANCH`、`IP2REGION_SYSTEMD_SERVICE`、`IP2REGION_PID_FILE`、`IP2REGION_RELOAD_SIGNAL`、`IP2REGION_XDB_MIN_SIZE` 等。

## 部署

### systemd

1. 将二进制与 `config.yaml` 放到 `/opt/ip2region/`，xdb 放到配置指定的路径。
2. 复制单元文件并启用：

```bash
cp deploy/ip2region.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now ip2region
```

3. 更新数据后热加载：`systemctl reload ip2region`（等价于向进程发 `SIGHUP`）。

> 若 `config.yaml` 中配置了 `pid_file`，可在单元中开启 `PIDFile=` 与之对应（脚本亦可直接读该 PID 文件发信号）。

### Docker

```bash
docker build -t go-ip2region .
docker run -d -p 8080:8080 \
  -v /path/to/data:/data \
  -e IP2REGION_V4_XDB=/data/ip2region_v4.xdb \
  -e IP2REGION_V6_XDB=/data/ip2region_v6.xdb \
  go-ip2region
```

## 发布

打 `v*` 标签并推送即触发 GitHub Actions，自动交叉编译常用平台二进制并发布为 GitHub Release 资产：

- 平台：linux / darwin / windows × amd64 / arm64（共 6 种）
- 产物命名：`go-ip2region-<os>-<arch>.tar.gz`（Windows 为 `.zip`），内置版本号取自标签
- 查看版本：`./go-ip2region -version`

```bash
git tag v0.1.0
git push origin v0.1.0
```

## 性能说明

- 默认 `content` 全内存缓存，单查询约 10μs 级，无磁盘 IO。
- searcher 池大小（`searchers`）同时是并发上限，过载时请求等待而非无限扩容，保护稳定性；可按负载通过 `IP2REGION_SEARCHERS` 调整。
- 批量查询在池大小约束下并发执行，充分利用多核，结果有序返回。
- 输入在进入查询前经 `net.ParseIP` 校验，非法请求被快速拒绝，避免无效占用。

## 许可证

服务代码采用 Apache License 2.0；xdb 数据版权归 ip2region 项目所有，请遵循其数据使用条款。
