// Package ingest turns events received over gRPC/Connect or the Sentry
// protocol into stored issues and events.
//
// The flow is: authenticate the key → rate-limit → normalize (trim, scrub,
// enrich, classify frames) → fingerprint → enqueue on a per-project shard →
// group-commit in batches (issues upsert, events insert, counters, facets,
// alert outbox) → acknowledge the waiting callers → publish live updates.
package ingest

// Size limits applied during normalization. They match the limits documented
// in proto/izci/ingest/v1/event.proto; data beyond them is trimmed and
// reported as a warning rather than rejected.
const (
	maxEventBytes = 1 << 20

	maxShortString  = 64
	maxName         = 200
	maxLogger       = 128
	maxTransaction  = 256
	maxServerName   = 128
	maxRelease      = 200
	maxPlatform     = 32
	maxMessage      = 8 << 10
	maxTemplate     = 1 << 10
	maxParams       = 32
	maxParamLen     = 1 << 10
	maxExceptions   = 10
	maxTypeLen      = 256
	maxValueLen     = 8 << 10
	maxFrames       = 250
	keepOldFrames   = 50
	maxFrameField   = 256
	maxFilename     = 512
	maxAbsPath      = 1 << 10
	maxContextLine  = 1 << 10
	maxContextLines = 5
	maxFrameVars    = 50
	maxVarLen       = 1 << 10
	maxThreads      = 64
	maxBreadcrumbs  = 100
	maxCrumbData    = 8 << 10
	maxURL          = 4 << 10
	maxRoute        = 512
	maxQuery        = 4 << 10
	maxHeaders      = 100
	maxHeaderValue  = 2 << 10
	maxCookies      = 50
	maxBody         = 16 << 10
	maxEnvVars      = 50
	maxAttributes   = 20
	maxCustomBlocks = 20
	maxCustomBlock  = 8 << 10
	maxTags         = 50
	maxTagKey       = 64
	maxTagValue     = 200
	maxExtra        = 64 << 10
	maxFingerprint  = 16
	maxModules      = 500
	maxDBStatement  = 8 << 10
	maxDBParams     = 50
	maxCommandLine  = 2 << 10
	maxGenericField = 256
	maxDataEntries  = 20
)
