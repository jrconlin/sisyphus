/* Insert Moz Header
 */

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"database/sql"
	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
)

/* Forever pushing */
const (
	PROJECT   = "sisyphus"
	VERSION   = "0.1"
	CHANNELS  = 255
	DATA_SIZE = 1024
)

var (
	host         = flag.String("addr", "http://localhost:8080", "Local server URL (e.g. http://localhost:8080)")
	dbPath       = flag.String("db", fmt.Sprintf("%s.db", PROJECT), "Path to database file")
	templatePath = flag.String("templates", "templates", "Path to templates")
	db           *sql.DB
	logger       logWriter
)

type logWriter struct {
	enabled bool
}

func (r *logWriter) Error(str string, arg ...interface{}) {
	label := "ERROR: " + str
	if arg != nil {
		log.Printf(label, arg)
		return
	}
	log.Print(label)
	return
}

func (r *logWriter) Log(str string, arg ...interface{}) {
	label := "       " + str
	if arg != nil {
		log.Printf(label, arg)
		return
	}
	log.Printf(label)
	return
}

type command struct {
	Action   string      `json:"action"`
	Argument interface{} `json:"arg"`
}

type store struct {
	log *logWriter
	db  *sql.DB
	cmd chan []byte
}

func (s *store) run() {
	var err error
	for {
		select {
		case c := <-s.cmd:
			if len(c) == 0 {
				continue
			}
			cmd := command{}
			err = json.Unmarshal(c, &cmd)
			if err != nil {
				e, _ := json.Marshal(command{
					Action:   "error",
					Argument: err.Error(),
				})
				s.cmd <- e
			}
			switch strings.ToLower(cmd.Action) {
			default:
				e, _ := json.Marshal(command{
					Action:   "error",
					Argument: "Unsupported command",
				})
				s.cmd <- e
			}
		}
	}
}

type hub struct {
	connections map[*connection]bool
	broadcast   chan []byte
	register    chan *connection
	unregister  chan *connection
	db          *sql.DB
	log         *logWriter
}

var h = hub{
	broadcast:   make(chan []byte),
	register:    make(chan *connection),
	unregister:  make(chan *connection),
	connections: make(map[*connection]bool),
}

func (h *hub) Count() int {
	return len(h.connections)
}

func (h *hub) run(st *store) {
	h.db = st.db
	for {
		select {
		case c := <-h.register:
			h.log.Log("Registering...")
			h.connections[c] = true
			c.log = h.log
		case c := <-h.unregister:
			if _, ok := h.connections[c]; ok {
				h.log.Log("Unregistering...")
				delete(h.connections, c)
				close(c.cmd)
			}
		case m := <-h.broadcast:
			h.log.Log("Broadcasting...")
			m = bytes.Map(func(r rune) rune {
				if r < ' ' {
					return -1
				}
				return r
			}, m)
			for c := range h.connections {
				c.cmd <- m
			}
		}
	}
}

type connection struct {
	ws  *websocket.Conn
	cmd chan []byte
	db  *sql.DB
	log *logWriter
}

func (c *connection) reader() {
	defer c.ws.Close()
	for {
		_, raw, err := c.ws.ReadMessage()
		log.Printf("Message: %s\n", raw)
		if err != nil {
			c.log.Error("Reader failure %s", err.Error())
			return
		}

		cmd := command{}
		if err := json.Unmarshal(raw, &cmd); err != nil {
			c.log.Error("Could not process command %s", string(raw))
			return
		}
		switch strings.ToLower(cmd.Action)[:2] {
		default:
			c.log.Error("Unknown Command sent from client %+v", cmd)
		}
	}
}

func (c *connection) writer() {
	for command := range c.cmd {
		err := c.ws.WriteMessage(websocket.TextMessage, command)
		if err != nil {
			break
		}
	}
	c.ws.Close()
}

var upgrader = &websocket.Upgrader{
	ReadBufferSize:  DATA_SIZE,
	WriteBufferSize: DATA_SIZE,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

func wsHandler(resp http.ResponseWriter, req *http.Request) {
	logger.Log("New websocket connection...")
	ws, err := upgrader.Upgrade(resp, req, nil)
	if err != nil {
		logger.Error("Upgrade failed: %s", err.Error())
		return
	}

	conn := &connection{
		cmd: make(chan []byte, CHANNELS),
		ws:  ws,
		log: &logger,
	}

	logger.Log("Connecting to %s", req.RemoteAddr)
	h.register <- conn
	defer func() { h.unregister <- conn }()
	go conn.writer()
	logger.Log("Starting reader...")
	conn.reader()
}

func main() {
	var err error
	flag.Parse()

	logger := &logWriter{}
	h.log = logger

	if db, err = sql.Open("sqlite3", *dbPath); err != nil {
		log.Fatal("Could not open db: %s", err.Error())
	}

	st := &store{
		db:  db,
		cmd: make(chan []byte),
		log: logger,
	}

	go st.run()
	go h.run(st)

	fatal := func(resp http.ResponseWriter, err error) {
		const errTmpl = "Could not render index page: %s"
		errstr := fmt.Sprintf(errTmpl, err)
		logger.Error(errstr)
		http.Error(resp, errstr, 500)
	}

	// Index page handler.
	http.HandleFunc("/", func(resp http.ResponseWriter, req *http.Request) {
		logger.Log("index page")
		// display a fatal
		t, err := template.ParseFiles(filepath.Join(*templatePath, "index.tmpl"))
		if err != nil {
			fatal(resp, err)
			return
		}
		err = t.Execute(resp, struct{ Host string }{Host: *host})
		if err != nil {
			fatal(resp, err)
		}
		return
	})

	// Socket for page updates.
	http.HandleFunc("/ws", wsHandler)

	hostAddr, err := url.Parse(*host)
	if err != nil {
		logger.Error("Invalid host argument: %s", err.Error())
		return
	}

	logger.Log("Staring up server at %s\n", hostAddr.Host)
	if err := http.ListenAndServe(hostAddr.Host, nil); err != nil {
		logger.Error("Server failed: %s", err.Error())
	}
}
