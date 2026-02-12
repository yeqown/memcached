package telemetry

import "go.opentelemetry.io/otel/attribute"

// Semantic attribute keys following OpenTelemetry Database Semantic Conventions
var (
	attrDBSystem     = attribute.Key("db.system")
	attrDBOperation  = attribute.Key("db.operation")
	attrNetPeerName  = attribute.Key("net.peer.name")
	attrNetPeerPort  = attribute.Key("net.peer.port")
	attrNetTransport = attribute.Key("net.transport")
	attrMemcachedKey = attribute.Key("memcached.key")
)
