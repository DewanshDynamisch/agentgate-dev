package toolcatalog

import (
	"context"
	"os"
	"testing"
)

func TestPostgresStore(t *testing.T) {
	connStr := os.Getenv("AGENTGATE_TEST_POSTGRES_URL")
	if connStr == "" {
		t.Skip("skipping PostgresStore test: AGENTGATE_TEST_POSTGRES_URL not set")
	}
	ctx := context.Background()
	s, err := NewPostgresStore(ctx, connStr)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer s.Close()
	for i := 0; i < 2; i++ {
		if err := s.Migrate(ctx); err != nil {
			t.Fatalf("Migrate run %d: %v", i+1, err)
		}
	}
	if _, err := s.pool.Exec(ctx, "DELETE FROM tool_definitions"); err != nil {
		t.Fatal(err)
	}
	runStoreBehavior(t, s)
}
