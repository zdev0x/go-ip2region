#!/usr/bin/env bash
#
# ip2region xdb 数据更新脚本
# ----------------------------------------------------------------------------
# 用途：定时（crontab）下载官方最新 xdb，校验后原子替换到目标目录，
#       并向运行中的服务发送 SIGHUP 触发热加载（无需重启进程，零停机）。
#
# 示例 crontab（每日 03:17 执行，输出追加到日志）：
#   17 3 * * * /opt/ip2region/scripts/update-xdb.sh >> /var/log/ip2region-update.log 2>&1
#
# 行为说明：
#   1. 从官方仓库下载 v4 / v6 xdb（GitHub LFS，经 raw 域名获取真实二进制）。
#   2. 校验：体积达标且非 LFS 指针 / 错误页，避免用坏数据覆盖。
#   3. 在目标目录内创建临时文件并 mv 原子替换（同文件系统下 rename 原子）。
#   4. 通过 PID 文件或服务名通知服务热加载；均未配置则仅更新文件。
#
# 可通过环境变量覆盖下方任意配置项。
# ----------------------------------------------------------------------------
set -euo pipefail

# ------------------------- 可配置项（环境变量可覆盖） -------------------------
XDB_DIR="${IP2REGION_XDB_DIR:-/data}"
V4_NAME="${IP2REGION_V4_NAME:-ip2region_v4.xdb}"
V6_NAME="${IP2REGION_V6_NAME:-ip2region_v6.xdb}"
BRANCH="${IP2REGION_XDB_BRANCH:-master}"
BASE_URL="${IP2REGION_XDB_BASE:-https://raw.githubusercontent.com/lionsoul2014/ip2region/${BRANCH}/data}"

# 通知方式：优先 systemd 服务名，其次 PID 文件；均空则仅更新文件
SYSTEMD_SERVICE="${IP2REGION_SYSTEMD_SERVICE:-}"
PID_FILE="${IP2REGION_PID_FILE:-/run/ip2region.pid}"
RELOAD_SIGNAL="${IP2REGION_RELOAD_SIGNAL:-HUP}"

# 单文件最小体积（字节），用于快速过滤 LFS 指针 / 错误页，默认 1MB
MIN_SIZE="${IP2REGION_XDB_MIN_SIZE:-1048576}"
# ---------------------------------------------------------------------------

log() { echo "[$(date '+%Y-%m-%d %H:%M:%S')] $*"; }

# 下载并校验单个文件，成功时落盘到 $1（调用方负责路径）
fetch() {
  local name="$1" dest="$2"
  log "下载 $name ..."
  if ! curl -fsSL --retry 3 --retry-delay 5 --retry-all-errors "$BASE_URL/$name" -o "$dest"; then
    log "ERROR: 下载 $name 失败"
    return 1
  fi
  local size
  size=$(stat -c%s "$dest")
  if [ "$size" -lt "$MIN_SIZE" ]; then
    log "ERROR: $name 体积异常（${size} 字节），疑似 LFS 指针或下载失败"
    return 1
  fi
  # LFS 指针文件首行形如 "version https://git-lfs.github.com/spec/v1"
  if head -c 64 "$dest" | grep -q "git-lfs"; then
    log "ERROR: $name 为 LFS 指针文件，未获取到真实二进制"
    return 1
  fi
  log "下载完成 $name（${size} 字节）"
  return 0
}

mkdir -p "$XDB_DIR"
TMP_DIR="$(mktemp -d "$XDB_DIR/.xdb-update.XXXXXX")"
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT

if ! fetch "$V4_NAME" "$TMP_DIR/$V4_NAME"; then cleanup; exit 1; fi
if ! fetch "$V6_NAME" "$TMP_DIR/$V6_NAME"; then cleanup; exit 1; fi

# 原子替换（同文件系统内 rename 为原子操作）
if ! mv -f "$TMP_DIR/$V4_NAME" "$XDB_DIR/$V4_NAME" || ! mv -f "$TMP_DIR/$V6_NAME" "$XDB_DIR/$V6_NAME"; then
  log "ERROR: 替换 xdb 文件失败"
  exit 1
fi
log "xdb 文件已更新至 $XDB_DIR"

# 通知服务热加载
if [ -n "$SYSTEMD_SERVICE" ]; then
  log "通过 systemd 重载 $SYSTEMD_SERVICE"
  if systemctl reload "$SYSTEMD_SERVICE"; then
    log "已发送 reload"
  else
    log "WARN: systemctl reload 失败，请检查服务单元配置（ExecReload 需发送 SIGHUP）"
  fi
elif [ -f "$PID_FILE" ]; then
  PID="$(cat "$PID_FILE" 2>/dev/null || true)"
  if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
    log "向 PID=$PID 发送 SIG${RELOAD_SIGNAL}"
    if kill "-${RELOAD_SIGNAL}" "$PID"; then
      log "已发送信号"
    else
      log "WARN: 发送 SIG${RELOAD_SIGNAL} 失败"
    fi
  else
    log "WARN: PID 文件指向的进程不存在（$PID），跳过发信号"
  fi
else
  log "WARN: 未配置 SYSTEMD_SERVICE / PID_FILE，仅更新文件，未通知服务"
fi

log "完成"
