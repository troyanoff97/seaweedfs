package cassandra

import (
	"context"
	"testing"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

func TestResolveCassandraTimeoutsDefaults(t *testing.T) {
	q, c := resolveCassandraTimeouts(0, 0)
	if q != 10*time.Second {
		t.Fatalf("query timeout=%v want 10s", q)
	}
	if c != 5*time.Second {
		t.Fatalf("connect timeout=%v want 5s", c)
	}
}

func TestResolveCassandraTimeoutsCapsConnectToQuery(t *testing.T) {
	q, c := resolveCassandraTimeouts(3000, 10000)
	if q != 3*time.Second {
		t.Fatalf("query timeout=%v want 3s", q)
	}
	if c != 3*time.Second {
		t.Fatalf("connect timeout=%v want 3s (capped to query)", c)
	}
}

func TestResolveCassandraTimeoutsHonorsConfigured(t *testing.T) {
	q, c := resolveCassandraTimeouts(8000, 2000)
	if q != 8*time.Second || c != 2*time.Second {
		t.Fatalf("got query=%v connect=%v", q, c)
	}
}

func TestParseCassandraConsistency(t *testing.T) {
	if parseCassandraConsistency("") != gocql.LocalQuorum {
		t.Fatal("empty should be LOCAL_QUORUM")
	}
	if parseCassandraConsistency("local_one") != gocql.LocalOne {
		t.Fatal("LOCAL_ONE")
	}
	if parseCassandraConsistency("ONE") != gocql.One {
		t.Fatal("ONE")
	}
}

func TestQueryContextAppliesDeadline(t *testing.T) {
	store := &CassandraStore{queryTimeout: 50 * time.Millisecond}
	ctx, cancel := store.queryContext(context.Background())
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected deadline")
	}
	if time.Until(deadline) > 60*time.Millisecond {
		t.Fatalf("deadline too far: %v", time.Until(deadline))
	}
}

func TestQueryContextKeepsCallerDeadline(t *testing.T) {
	store := &CassandraStore{queryTimeout: time.Second}
	parent, parentCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer parentCancel()
	ctx, cancel := store.queryContext(parent)
	defer cancel()
	if ctx != parent {
		t.Fatal("expected caller context to be preserved")
	}
}
