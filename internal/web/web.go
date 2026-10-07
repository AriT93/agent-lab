// Package web is a browser chat UI for the stage 3 agent, built with Gin.
//
// The lesson is where an agent lives in a web app. The agent is a plain Go
// value that holds one conversation, so the server keeps one per browser
// (found by a cookie) and calls it in-process from the handler. There is no
// joke API and no MCP hop: MCP is for other programs (Claude Code) to borrow
// the tools, and this app already has them. The page is server-rendered; the
// only JavaScript posts the form and appends the HTML fragment that comes back.
//
// Each session's -trace output is captured into a buffer and shown under the
// reply, so you can watch prompts, tool calls and token counts in the browser.
package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"html/template"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/AriT93/agent-lab/internal/dadjoke"
	"github.com/AriT93/agent-lab/internal/jokeapi"
	"github.com/AriT93/agent-lab/internal/ollama"
	"github.com/AriT93/agent-lab/internal/stage3"
	"github.com/AriT93/agent-lab/internal/trace"
)

//go:embed templates/*.html
var templates embed.FS

const cookie = "agent-lab-session"

// maxSessions bounds memory: every session holds a conversation. The oldest
// is dropped past this, which just means that browser starts a fresh chat.
const maxSessions = 20

type session struct {
	mu    sync.Mutex // one message at a time per conversation
	agent *stage3.Agent
	buf   bytes.Buffer // trace output since the last reply
	used  time.Time
}

type Server struct {
	LLM   *ollama.Client
	Jokes *jokeapi.Client
	Dads  *dadjoke.Client

	mu       sync.Mutex
	sessions map[string]*session
}

func New(llm *ollama.Client, jokes *jokeapi.Client, dads *dadjoke.Client) *Server {
	return &Server{LLM: llm, Jokes: jokes, Dads: dads, sessions: map[string]*session{}}
}

func (s *Server) Handler() http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.SetHTMLTemplate(template.Must(template.ParseFS(templates, "templates/*.html")))
	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") }) // for container and load balancer health checks
	r.GET("/", s.index)
	r.POST("/chat", s.chat)
	r.POST("/reset", s.reset)
	return r
}

func (s *Server) index(c *gin.Context) {
	s.session(c) // set the cookie
	c.HTML(http.StatusOK, "index.html", nil)
}

func (s *Server) chat(c *gin.Context) {
	text := strings.TrimSpace(c.PostForm("text"))
	if text == "" {
		c.Status(http.StatusNoContent)
		return
	}
	ss := s.session(c)
	ss.mu.Lock()
	defer ss.mu.Unlock()

	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Minute)
	defer cancel()
	reply, err := ss.agent.Respond(ctx, text)
	data := gin.H{"User": text, "Reply": reply, "Trace": ansi.ReplaceAllString(ss.buf.String(), "")}
	ss.buf.Reset()
	if err != nil {
		data["Error"] = err.Error()
	}
	c.HTML(http.StatusOK, "turn.html", data)
}

func (s *Server) reset(c *gin.Context) {
	ss := s.session(c)
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.agent.Reset()
	ss.buf.Reset()
	c.Status(http.StatusNoContent)
}

// session finds the caller's conversation or starts a new one.
func (s *Server) session(c *gin.Context) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, err := c.Cookie(cookie)
	ss, ok := s.sessions[id]
	if err != nil || !ok {
		id = newID()
		ss = &session{agent: stage3.New(s.LLM, s.Jokes, s.Dads)}
		ss.agent.Trace = trace.New(&ss.buf)
		s.sessions[id] = ss
		if len(s.sessions) > maxSessions {
			s.evictOldest(id)
		}
		c.SetSameSite(http.SameSiteLaxMode)
		c.SetCookie(cookie, id, 0, "/", "", false, true)
	}
	ss.used = time.Now()
	return ss
}

func (s *Server) evictOldest(keep string) {
	var oldest string
	for id, ss := range s.sessions {
		if id != keep && (oldest == "" || ss.used.Before(s.sessions[oldest].used)) {
			oldest = id
		}
	}
	delete(s.sessions, oldest)
}

func newID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ansi matches the colour codes trace.Tracer wraps its output in.
var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)
