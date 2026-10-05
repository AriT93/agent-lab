package jokeapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
)

func TestRequestURL(t *testing.T) {
	tests := []struct {
		name string
		req  Request
		want string
	}{
		{"zero value", Request{}, "https://x/joke/Any"},
		{"one category", Request{Categories: []Category{Programming}}, "https://x/joke/Programming"},
		{"many categories", Request{Categories: []Category{Pun, Spooky}}, "https://x/joke/Pun,Spooky"},
		{"type", Request{Type: "twopart"}, "https://x/joke/Any?type=twopart"},
		{"type any is dropped", Request{Type: "any"}, "https://x/joke/Any"},
		{
			"blacklist and contains",
			Request{Blacklist: []Flag{NSFW, Racist}, Contains: "cubs"},
			"https://x/joke/Any?blacklistFlags=nsfw%2Cracist&contains=cubs",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.req.URL("https://x/joke/"); got != tt.want {
				t.Errorf("URL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func fakeServer(t *testing.T, status int, body string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, HTTP: srv.Client()}
}

func TestFetch(t *testing.T) {
	ctx := context.Background()

	t.Run("single", func(t *testing.T) {
		c := fakeServer(t, 200, `{"error":false,"type":"single","joke":"light attracts bugs","id":1}`)
		j, err := c.Fetch(ctx, Request{})
		if err != nil {
			t.Fatal(err)
		}
		if j.Text() != "light attracts bugs" {
			t.Errorf("Text() = %q", j.Text())
		}
	})

	t.Run("twopart", func(t *testing.T) {
		c := fakeServer(t, 200, `{"error":false,"type":"twopart","setup":"Q?","delivery":"A!","id":2}`)
		j, err := c.Fetch(ctx, Request{})
		if err != nil {
			t.Fatal(err)
		}
		if j.Text() != "Q?\n\nA!" {
			t.Errorf("Text() = %q", j.Text())
		}
	})

	t.Run("no match is an APIError", func(t *testing.T) {
		c := fakeServer(t, 400, `{"error":true,"code":106,"message":"No matching joke found","causedBy":["No jokes were found"]}`)
		_, err := c.Fetch(ctx, Request{Contains: "zzz"})
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.Code != 106 {
			t.Fatalf("want APIError code 106, got %v", err)
		}
	})

	t.Run("HTTP error without JSON", func(t *testing.T) {
		c := fakeServer(t, 500, `oops`)
		if _, err := c.Fetch(ctx, Request{}); err == nil {
			t.Fatal("want error")
		}
	})

	t.Run("unknown type", func(t *testing.T) {
		c := fakeServer(t, 200, `{"error":false,"type":"limerick"}`)
		if _, err := c.Fetch(ctx, Request{}); err == nil {
			t.Fatal("want error")
		}
	})
}

// TestLive hits the real API. Run with: LIVE=1 go test ./internal/jokeapi -run Live -v
func TestLive(t *testing.T) {
	if os.Getenv("LIVE") == "" {
		t.Skip("set LIVE=1 to call the real JokeAPI")
	}
	j, err := New().Fetch(context.Background(), Request{
		Categories: []Category{Programming},
		Type:       "twopart",
		Blacklist:  []Flag{NSFW, Racist},
	})
	if err != nil {
		t.Fatal(err)
	}
	if j.Category != "Programming" || j.Type != "twopart" || j.Flags["nsfw"] {
		t.Errorf("filters not honored: %+v", j)
	}
	t.Log(j.Text())
}

func TestNormalize(t *testing.T) {
	in := Request{
		Categories: []Category{"programming", "Sports", Programming},
		Type:       "any",
		Blacklist:  []Flag{Political, Political, "violent"},
		Contains:   " cubs ",
	}
	want := Request{Categories: []Category{Programming}, Blacklist: []Flag{Political}, Contains: "cubs"}
	if got := in.Normalize(); !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
