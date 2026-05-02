package gravity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentuity/go-common/logger"
)

func TestDoHealthProbeSetsGravityUserAgent(t *testing.T) {
	t.Parallel()

	var gotUserAgent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	g := &GravityClient{
		context: ctx,
		logger:  logger.NewTestLogger(),
	}

	status, err := g.doHealthProbe(srv.URL)
	if err != nil {
		t.Fatalf("doHealthProbe returned error: %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, status)
	}
	if gotUserAgent != GravityHealthProbeUserAgent {
		t.Fatalf("expected User-Agent %q, got %q", GravityHealthProbeUserAgent, gotUserAgent)
	}
}
