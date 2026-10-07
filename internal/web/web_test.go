package web

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/AriT93/agent-lab/internal/dadjoke"
	"github.com/AriT93/agent-lab/internal/jokeapi"
	"github.com/AriT93/agent-lab/internal/ollama"
)

// testServer fakes Ollama (replying "<n>: joke" or calling the dad joke tool
// first) and the joke sources, and returns the web app in front of them.
func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollama.ChatRequest
		json.NewDecoder(r.Body).Decode(&req)
		msg := ollama.Message{Role: "assistant", Content: "Why did the <script> cross the road?"}
		if last := req.Messages[len(req.Messages)-1]; last.Role == "user" {
			var tc ollama.ToolCall
			tc.Function.Name = "search_dad_jokes"
			tc.Function.Arguments = json.RawMessage(`{"query":"dog"}`)
			msg = ollama.Message{Role: "assistant", ToolCalls: []ollama.ToolCall{tc}}
		}
		calls.Add(1)
		json.NewEncoder(w).Encode(ollama.ChatResponse{Message: msg, DoneReason: "stop", PromptEvalCount: 100})
	}))
	t.Cleanup(model.Close)
	src := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"results":[{"id":"d1","joke":"A dog joke."}]}`))
	}))
	t.Cleanup(src.Close)

	llm := ollama.New()
	llm.BaseURL = model.URL
	s := New(llm, &jokeapi.Client{BaseURL: src.URL, HTTP: src.Client()}, &dadjoke.Client{BaseURL: src.URL, HTTP: src.Client()})
	app := httptest.NewServer(s.Handler())
	t.Cleanup(app.Close)
	return app
}

func post(t *testing.T, c *http.Client, url, text string) (int, string) {
	t.Helper()
	res, err := c.PostForm(url, map[string][]string{"text": {text}})
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := res.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return res.StatusCode, b.String()
}

func TestChatRendersReplyAndTrace(t *testing.T) {
	app := testServer(t)
	jar, _ := cookieJar(app.URL)
	status, body := post(t, jar, app.URL+"/chat", "a dog joke")
	if status != 200 {
		t.Fatalf("status = %d, body %s", status, body)
	}
	for _, want := range []string{"a dog joke", "cross the road", "<details", "search_dad_jokes"} {
		if !strings.Contains(body, want) {
			t.Errorf("response missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<script>") || strings.Contains(body, "\x1b[") {
		t.Errorf("model output not escaped, or ANSI left in trace:\n%s", body)
	}
}

func TestSessionsAreSeparateAndReset(t *testing.T) {
	app := testServer(t)
	a, _ := cookieJar(app.URL)
	b, _ := cookieJar(app.URL)
	// The stage 3 trace reports how many user turns the agent remembers.
	turns := func(c *http.Client, text string) string {
		_, body := post(t, c, app.URL+"/chat", text)
		return body
	}
	turns(a, "first")
	if body := turns(b, "second"); !strings.Contains(body, "1 turns in memory") {
		t.Errorf("session B shares A's conversation:\n%s", body)
	}
	if body := turns(a, "again"); !strings.Contains(body, "2 turns in memory") {
		t.Errorf("session A lost its history:\n%s", body)
	}
	if res, _ := a.Post(app.URL+"/reset", "", nil); res.StatusCode != http.StatusNoContent {
		t.Errorf("reset status = %d", res.StatusCode)
	}
	if body := turns(a, "fresh"); !strings.Contains(body, "1 turns in memory") {
		t.Errorf("history survived reset:\n%s", body)
	}
}

func TestEmptyMessageIsIgnored(t *testing.T) {
	app := testServer(t)
	jar, _ := cookieJar(app.URL)
	if status, _ := post(t, jar, app.URL+"/chat", "  "); status != http.StatusNoContent {
		t.Errorf("status = %d, want 204", status)
	}
}

func TestIndexSetsCookie(t *testing.T) {
	app := testServer(t)
	res, err := http.Get(app.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || len(res.Cookies()) == 0 || res.Cookies()[0].Name != cookie {
		t.Errorf("status %d, cookies %v", res.StatusCode, res.Cookies())
	}
}

func cookieJar(string) (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	return &http.Client{Jar: jar}, err
}
