// Command jokes-web serves a browser chat UI for the stage 3 agent.
//
//	go run ./cmd/jokes-web   # then open http://localhost:8080
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/AriT93/agent-lab/internal/dadjoke"
	"github.com/AriT93/agent-lab/internal/jokeapi"
	"github.com/AriT93/agent-lab/internal/ollama"
	"github.com/AriT93/agent-lab/internal/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address (localhost only by default: there is no auth)")
	model := flag.String("model", "", "ollama model (default: "+ollama.DefaultModel+")")
	numCtx := flag.Int("ctx", 8192, "ollama: context window in tokens; bigger costs memory")
	keepAlive := flag.String("keep-alive", "2m", "ollama: how long the model stays loaded when idle")
	healthcheck := flag.Bool("healthcheck", false, "GET /healthz on -addr and exit 0 if it answers (for container health checks: the image has no curl)")
	flag.Parse()

	if *healthcheck {
		os.Exit(probe(*addr))
	}

	llm := ollama.New()
	llm.NumCtx, llm.KeepAlive = *numCtx, *keepAlive
	if *model != "" {
		llm.Model = *model
	}

	srv := &http.Server{Addr: *addr, Handler: web.New(llm, jokeapi.New(), dadjoke.New()).Handler()}

	// Free the model's memory on the way out, like cmd/jokes does.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
		llm.Unload(shutdown)
	}()

	fmt.Printf("agent-lab web · %s · ctx %d · keep-alive %s\nopen http://%s\n", llm.Model, llm.NumCtx, llm.KeepAlive, *addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// probe asks a running server for /healthz. A wildcard bind address like
// 0.0.0.0 is not dialable everywhere, so it probes localhost instead.
func probe(addr string) int {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return 2
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := http.Client{Timeout: 3 * time.Second}
	res, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return 1
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
