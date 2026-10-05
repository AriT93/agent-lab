package dadjoke

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func fake(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Errorf("Accept header = %q", r.Header.Get("Accept"))
		}
		switch {
		case r.URL.Path == "/":
			w.Write([]byte(`{"id":"r1","joke":"random one","status":200}`))
		case r.URL.Query().Get("term") == "dog":
			w.Write([]byte(`{"results":[{"id":"d1","joke":"a dog joke"}]}`))
		default:
			w.Write([]byte(`{"results":[]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, HTTP: srv.Client()}
}

func TestClient(t *testing.T) {
	c, ctx := fake(t), context.Background()

	if j, err := c.Random(ctx); err != nil || j.ID != "r1" {
		t.Errorf("Random() = %+v, %v", j, err)
	}
	if j, err := c.Search(ctx, "dog"); err != nil || j.ID != "d1" {
		t.Errorf("Search(dog) = %+v, %v", j, err)
	}
	if _, err := c.Search(ctx, "cubs"); !errors.Is(err, ErrNoMatch) {
		t.Errorf("Search(cubs) err = %v, want ErrNoMatch", err)
	}
}

// Run with: LIVE=1 go test ./internal/dadjoke -v
func TestLive(t *testing.T) {
	if os.Getenv("LIVE") == "" {
		t.Skip("set LIVE=1 to call the real API")
	}
	j, err := New().Search(context.Background(), "dog")
	if err != nil {
		t.Fatal(err)
	}
	t.Log(j.Joke)
}
