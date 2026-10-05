# memcached

[English](./README.md) | [简体中文](./README.zh-CN.md)

[![Go Reference](https://pkg.go.dev/badge/github.com/yeqown/memcached.svg)](https://pkg.go.dev/github.com/yeqown/memcached) [![构建状态](https://github.com/yeqown/memcached/workflows/Go/badge.svg)](https://github.com/yeqown/memcached/actions) [![许可证](https://img.shields.io/github/license/yeqown/memcached)](./LICENSE)

这是一个支持 Memcached 文本协议和 Meta 文本协议的 Go 客户端，提供逐请求 Context、多节点路由、可配置连接池、值编解码器，以及可选的 OpenTelemetry 观测能力。需要 Go 1.26 或更高版本。

## 与 gomemcache 对比

两个客户端都支持基础文本命令、CAS、多节点路由、连接复用、TCP 和 Unix socket。相对 [bradfitz/gomemcache](https://github.com/bradfitz/gomemcache) 的基础 API，本项目额外提供：

| 能力 | bradfitz/gomemcache | 本项目 |
| --- | --- | --- |
| 逐调用 Context 与截止时间 | `Get(key)` / `Set(item)` 不接收调用方的 Context | `Get(ctx, key)` / `Set(ctx, ...)`；支持 Context 截止时间，并可分别设置连接、读取、写入超时 |
| Meta 文本协议 | 没有 Meta 命令 API | `MetaGet`、`MetaSet`、`MetaDelete`、`MetaArithmetic`、`MetaDebug`、`MetaNoOp`；支持 CAS、TTL、失效与提前刷新标志 |
| 连接池限制 | 可设置 `MaxIdleConns` | 按节点设置最大连接数、最大空闲连接数、连接寿命和空闲超时 |
| 值压缩 | 没有内置编解码器 | 可插拔 `Codec`；兼容 MC-COMPRESS 的 Deflate、LZ4、Snappy、Zstd 压缩 |
| OpenTelemetry | 没有内置观测功能 | 按需启用链路追踪和操作指标 |
| 内置路由策略 | CRC32 `ServerList` 或自定义选择器 | CRC32、Murmur3、Rendezvous 哈希，或自定义地址解析器、节点选择器 |
| 配套工具 | 客户端库 | 交互式 [CLI](./cmd/memcached-cli/README.md) 和 [Wails GUI](./gui/README.md) |

对比依据为已核查的 gomemcache [提交 `24af94b`](https://github.com/bradfitz/gomemcache/tree/24af94b03874)；上游后续版本可能有变化。

API 还提供 `GetAndTouch`、`GetAndTouches`、`Stats`、`Version`，以及常见的存取、删除和计数命令。UDP 可按需启用。完整命令清单和使用限制见[进阶使用指南](./docs/usage.md)。

## 快速开始

先在 `localhost:11211` 启动 Memcached 服务，再安装客户端：

```bash
go get github.com/yeqown/memcached@latest
```

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/yeqown/memcached"
)

func main() {
	client, err := memcached.New("localhost:11211",
		memcached.WithDialTimeout(2*time.Second),
		memcached.WithReadTimeout(3*time.Second),
		memcached.WithWriteTimeout(3*time.Second),
		memcached.WithMaxConns(64),
		memcached.WithMaxIdleConns(16),
		memcached.WithMaxLifetime(time.Hour),
		memcached.WithMaxIdleTimeout(5*time.Minute),
	)
	if err != nil {
		panic(err)
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Set(ctx, "greeting", []byte("hello"), 0, time.Minute); err != nil {
		panic(err)
	}
	item, err := client.Get(ctx, "greeting")
	if err != nil {
		panic(err)
	}
	fmt.Println(string(item.Value)) // hello
}
```

以上参数只是示例，应按实际负载调整。`New` 还支持逗号分隔的服务地址，以及可以组合使用的选项：

| 配置目标 | 地址或选项 |
| --- | --- |
| 多节点与键路由 | `"cache-1:11211,cache-2:11211"`，配合 `memcached.WithPickBuilder(memcached.NewRendezvousHashPickBuilder(0))` |
| 自定义地址解析 | `memcached.WithResolver(resolver)` |
| 压缩或其他值编解码 | `memcached.WithCodec(codec)` |
| 链路追踪与指标 | `memcached.WithTelemetry(telemetry.WithTracerProvider(tp), telemetry.WithMeterProvider(mp))` |
| 写入时不等待服务端确认 | `memcached.WithNoReply()` |
| 旧式 UDP 传输 | `udp://` 地址配合 `memcached.WithUDPEnabled()` |

Codec 和 OpenTelemetry 的初始化见[进阶使用指南](./docs/usage.md)。使用多个节点时，`Gets` 和 `GetAndTouches` 会把所有请求的键发送到同一个节点；除非确认这些键在同一节点，否则请逐键读取。

## 更多资料

- [进阶使用指南：命令、路由、编解码、观测和传输方式](./docs/usage.md)
- [示例](./example/)与 [Go API 文档](https://pkg.go.dev/github.com/yeqown/memcached)
- [CLI 安装和命令](./cmd/memcached-cli/README.md)
- [MC-COMPRESS 标志格式](./docs/MC-COMPRESS-SPEC-v1.0.md)
