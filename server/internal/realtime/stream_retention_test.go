package realtime

import (
	"context"
	"errors"
	"testing"
	"time"

	redismock "github.com/go-redis/redismock/v9"
	"github.com/redis/go-redis/v9"
)

func TestStreamTTLRefresherRateLimitsPublishPath(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })

	now := time.Date(2026, 8, 19, 10, 0, 0, 0, time.UTC)
	refresher := newStreamTTLRefresher(15*time.Minute, 30*time.Second)
	refresher.now = func() time.Time { return now }

	mock.ExpectPExpire("stream", 15*time.Minute).SetVal(true)
	if err := refresher.refreshIfDue(context.Background(), rdb, "stream"); err != nil {
		t.Fatal(err)
	}
	if err := refresher.refreshIfDue(context.Background(), rdb, "stream"); err != nil {
		t.Fatal(err)
	}

	now = now.Add(30 * time.Second)
	mock.ExpectPExpire("stream", 15*time.Minute).SetVal(true)
	if err := refresher.refreshIfDue(context.Background(), rdb, "stream"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStreamTTLRefresherRetriesAfterFailure(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })

	refresher := newStreamTTLRefresher(15*time.Minute, 30*time.Second)
	mock.ExpectPExpire("stream", 15*time.Minute).SetErr(errors.New("redis unavailable"))
	if err := refresher.refreshIfDue(context.Background(), rdb, "stream"); err == nil {
		t.Fatal("expected PEXPIRE failure")
	}
	mock.ExpectPExpire("stream", 15*time.Minute).SetVal(true)
	if err := refresher.refreshIfDue(context.Background(), rdb, "stream"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStreamTTLRefresherRepairsOnlyMissingTTL(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })

	refresher := newStreamTTLRefresher(15*time.Minute, 30*time.Second)
	mock.ExpectPTTL("stream").SetVal(-1)
	mock.ExpectPExpire("stream", 15*time.Minute).SetVal(true)
	ttl, err := refresher.repairMissingTTL(context.Background(), rdb, "stream")
	if err != nil {
		t.Fatal(err)
	}
	if ttl != 15*time.Minute {
		t.Fatalf("repaired TTL = %s, want 15m", ttl)
	}

	mock.ExpectPTTL("stream").SetVal(10 * time.Minute)
	ttl, err = refresher.repairMissingTTL(context.Background(), rdb, "stream")
	if err != nil {
		t.Fatal(err)
	}
	if ttl != 10*time.Minute {
		t.Fatalf("healthy TTL = %s, want 10m", ttl)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestStreamTTLRefresherDisabledPersistsStreamForSafeRollback(t *testing.T) {
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })

	refresher := newStreamTTLRefresher(15*time.Minute, 30*time.Second)
	mock.ExpectPTTL("stream").SetVal(10 * time.Minute)
	mock.ExpectPersist("stream").SetVal(true)
	ttl, err := refresher.reconcileTTL(context.Background(), rdb, "stream", false)
	if err != nil {
		t.Fatal(err)
	}
	if ttl != -1 {
		t.Fatalf("TTL after disabled reconciliation = %s, want -1", ttl)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRedisInfoInt64(t *testing.T) {
	info := "# Memory\r\nused_memory:2160328\r\nmaxmemory:2097152\r\n"
	if got, ok := redisInfoInt64(info, "used_memory"); !ok || got != 2160328 {
		t.Fatalf("used_memory = %d, %v", got, ok)
	}
	if _, ok := redisInfoInt64(info, "missing"); ok {
		t.Fatal("expected missing field not to parse")
	}
}

func TestStreamIDLess(t *testing.T) {
	if !streamIDLess("10-9", "10-10") {
		t.Fatal("same millisecond, smaller sequence must be older")
	}
	if streamIDLess("10-10", "10-10") {
		t.Fatal("equal ids are not older")
	}
	if streamIDLess("100-0", "99-5") {
		t.Fatal("later millisecond must not be older")
	}
}

func TestTrimStreamBeforeUsesMinIDWhenRedisAcceptsIt(t *testing.T) {
	streamTrimMinIDUnsupported.Store(false)
	t.Cleanup(func() { streamTrimMinIDUnsupported.Store(false) })
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })

	mock.ExpectXTrimMinID("ws:relay:shard:0", "1000-0").SetVal(4)
	trimmed, err := trimStreamBefore(context.Background(), rdb, "ws:relay:shard:0", "1000-0")
	if err != nil {
		t.Fatal(err)
	}
	if trimmed != 4 {
		t.Fatalf("trimmed = %d, want 4", trimmed)
	}
	if streamTrimMinIDUnsupported.Load() {
		t.Fatal("a successful XTRIM MINID must keep using that command")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTrimStreamBeforeDeletesOlderEntriesWhenMinIDIsRejected(t *testing.T) {
	streamTrimMinIDUnsupported.Store(false)
	previousBatch := streamTrimDeleteBatch
	streamTrimDeleteBatch = 2
	t.Cleanup(func() {
		streamTrimMinIDUnsupported.Store(false)
		streamTrimDeleteBatch = previousBatch
	})
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })

	stream := "ws:relay:shard:0"
	minID := "3000-0"
	mock.ExpectXTrimMinID(stream, minID).SetErr(errors.New("ERR syntax error"))
	mock.ExpectXRangeN(stream, "-", minID, 2).SetVal([]redis.XMessage{{ID: "1000-0"}, {ID: "2000-1"}})
	mock.ExpectXDel(stream, "1000-0", "2000-1").SetVal(2)
	mock.ExpectXRangeN(stream, "2000-1", minID, 2).SetVal([]redis.XMessage{{ID: "2000-1"}, {ID: "3000-0"}})

	trimmed, err := trimStreamBefore(context.Background(), rdb, stream, minID)
	if err != nil {
		t.Fatal(err)
	}
	if trimmed != 2 {
		t.Fatalf("trimmed = %d, want 2", trimmed)
	}
	if !streamTrimMinIDUnsupported.Load() {
		t.Fatal("expected later trims to skip XTRIM MINID")
	}

	mock.ExpectXRangeN(stream, "-", minID, 2).SetVal([]redis.XMessage{{ID: "2500-0"}})
	mock.ExpectXDel(stream, "2500-0").SetVal(1)
	trimmed, err = trimStreamBefore(context.Background(), rdb, stream, minID)
	if err != nil {
		t.Fatal(err)
	}
	if trimmed != 1 {
		t.Fatalf("second trimmed = %d, want 1", trimmed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestTrimStreamBeforeReportsOtherRedisErrors(t *testing.T) {
	streamTrimMinIDUnsupported.Store(false)
	t.Cleanup(func() { streamTrimMinIDUnsupported.Store(false) })
	rdb, mock := redismock.NewClientMock()
	t.Cleanup(func() { _ = rdb.Close() })

	mock.ExpectXTrimMinID("ws:relay:shard:0", "1000-0").SetErr(errors.New("LOADING Redis is loading"))
	if _, err := trimStreamBefore(context.Background(), rdb, "ws:relay:shard:0", "1000-0"); err == nil {
		t.Fatal("expected the Redis error to be returned")
	}
	if streamTrimMinIDUnsupported.Load() {
		t.Fatal("an unrelated Redis error must not disable XTRIM MINID")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
