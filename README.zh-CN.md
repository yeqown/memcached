# memcached

[English](./README.md) | [简体中文](./README.zh-CN.md)

[![Go Reference](https://pkg.go.dev/badge/github.com/yeqown/memcached.svg)](https://pkg.go.dev/github.com/yeqown/memcached) [![构建状态](https://github.com/yeqown/memcached/workflows/Go/badge.svg)](https://github.com/yeqown/memcached/actions) [![许可证](https://img.shields.io/github/license/yeqown/memcached)](./LICENSE)

这是一个支持 Memcached 文本协议和 Meta 文本协议的 Go 客户端，提供逐请求 Context、由 Resolver 驱动的节点发现、多节点路由、可配置连接池、值编解码器，以及可选的 OpenTelemetry 观测能力。需要 Go 1.26 或更高版本。

## 与 gomemcache 对比

两个客户端都支持基础文本命令、CAS、多节点路由、连接复用、TCP 和 Unix socket。相对 [bradfitz/gomemcache](https://github.com/bradfitz/gomemcache) 的基础 API，本项目额外提供：

| 能力 | bradfitz/gomemcache | 本项目 |
| --- | --- | --- |
| 逐调用 Context 与截止时间 | `Get(key)` / `Set(item)` 不接收调用方的 Context | `Get(ctx, key)` / `Set(ctx, ...)`；支持 Context 截止时间，并可分别设置连接、读取、写入超时 |
| Meta 文本协议 | 没有 Meta 命令 API | `MetaGet`、`MetaSet`、`MetaDelete`、`MetaArithmetic`、`MetaDebug`、`MetaNoOp`；支持 CAS、TTL 等选项，但尚未暴露全部提前刷新响应标志 |
| 连接池限制 | 每个地址复用连接，可设置 `MaxIdleConns` | 每个节点一个池，可设置 `MaxConns`、连接寿命和空闲超时；并发拨号前预留容量 |
| 值压缩 | 没有内置编解码器 | 可插拔 `Codec`；兼容 MC-COMPRESS 的 Deflate、LZ4、Snappy、Zstd 压缩 |
| OpenTelemetry | 没有内置观测功能 | 按需启用链路追踪、操作指标和发现、拓扑指标 |
| 内置路由策略 | CRC32 `ServerList` 或自定义选择器 | CRC32、Murmur3、Rendezvous 哈希、稳定 Rendezvous 哈希，或自定义节点选择器 |
| 节点发现 | 通过选择器配置地址 | `resolver` 包内置静态解析、AWS / Google 自动发现；自定义 Resolver 决定下次刷新时间 |
| 配套工具 | 客户端库 | 交互式 [CLI](./cmd/memcached-cli/README.md) 和 [Wails GUI](./gui/README.md) |

对比依据为已核查的 gomemcache [提交 `4d751bb`](https://github.com/bradfitz/gomemcache/tree/4d751bb6e37cf0da5fd57a86b880f76791307adf)；上游后续版本可能有变化。

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
| 多节点与键路由 | `"cache-1:11211,cache-2:11211"`，配合 `memcached.WithPicker(picker.NewStableRendezvousHashPicker(0))` |
| 自定义地址解析与发现 | `memcached.WithResolver(resolver)` 和 `memcached.WithResolveTimeout(5*time.Second)` |
| 压缩或其他值编解码 | `memcached.WithCodec(codec)` |
| 链路追踪与指标 | `memcached.WithTelemetry(telemetry.WithTracerProvider(tp), telemetry.WithMeterProvider(mp))` |
| 写入时不等待服务端确认 | `memcached.WithNoReply()` |
| 旧式 UDP 传输 | `udp://` 地址配合 `memcached.WithUDPEnabled()` |

Codec 和 OpenTelemetry 的初始化见[进阶使用指南](./docs/usage.md)。使用多个节点时，`Gets` 和 `GetAndTouches` 会把所有请求的键发送到同一个节点；除非确认这些键在同一节点，否则请逐键读取。

AWS ElastiCache Memcached 和 Google Memorystore Memcached 可使用同一个内置 Resolver：将配置 / 发现端点（含端口）传给 `New`，并设置 `memcached.WithResolver(resolver.NewAutoDiscovery(time.Minute))`。它仅使用 `config get cluster`，按返回的版本和节点列表更新拓扑，错误时保留最近成功的结果。非正刷新周期默认一分钟。完整示例见[自动发现指南](./docs/usage.md#aws-and-google-automatic-discovery)。

## Resolver / Picker API 迁移

Resolver、Picker 及其内置实现分别位于 `github.com/yeqown/memcached/resolver`、`github.com/yeqown/memcached/picker`，地址类型由 `resolver.Addr` 定义。自定义 Resolver 需要实现 `Resolve(ctx context.Context, target string) (resolver.ResolveResult, *time.Time, error)`，通过 `ResolveResult.Addrs` 返回完整节点列表，并单独返回下次解析时间。时间为 nil 时停止刷新，返回 error 时也一样；暂时失败应同时返回重试时间。默认 `resolver.NewStatic()` 只解析一次。

自定义路由算法实现 `picker.Picker`，通过 `memcached.WithPicker(p)` 直接传入实例。Builder 接口已移除，`picker.NewCRC32HashPicker()` 等内置构造函数直接返回可用的 Picker。哈希函数收拢到 `picker` 内部，原 `hash` 包已移除。Client 在拓扑变更时复用传入的 Picker，每次 `Pick` 都传入当前不可变地址快照。Picker 必须支持并发调用，不能保存或修改传入的地址。地址会被规范化并排序，因此升级后已有键的分布可能变化。节点增减或切换哈希策略也可能造成缓存未命中，业务需要准备回源或预热。

## 更多资料

- [进阶使用指南：命令、路由、编解码、观测和传输方式](./docs/usage.md)
- [示例](./example/)与 [Go API 文档](https://pkg.go.dev/github.com/yeqown/memcached)
- [CLI 安装和命令](./cmd/memcached-cli/README.md)
- [MC-COMPRESS 标志格式](./docs/MC-COMPRESS-SPEC-v1.0.md)
