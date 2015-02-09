/* Insert Moz Header
 */

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
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
	localaddr    = flag.String("local", "0.0.0.0:8080", "Local server (e.g. 0.0.0.0:8080)")
	host         = flag.String("addr", "localhost:8080", "Remote address to use (e.g. 192.168.0.1:8080)")
	dbPath       = flag.String("db", fmt.Sprintf("%s.db", PROJECT), "Path to database file")
	templatePath = flag.String("templates", "template", "Path to templates")
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
	Action string      `json:"action"`
	Args   interface{} `json:"arg"`
}

const STORE_INSERT = `insert or abort into pings (url, touch) values (?, strftime('now');`
const STORE_CREATE = `create table if not exists pings (url string primary key, version integer, touch integer);`

type store struct {
	log *logWriter
	db  *sql.DB
	Cmd chan *command
}

func (s *store) init() (err error) {
	_, err = s.db.Exec(STORE_CREATE)
	return
}

func (s *store) Add(url string) (err error) {
	_, err = s.db.Exec(`insert or abort into pings (url, touched) values(?, now());`, url)
	return
}

func (s *store) Ack(url string) (err error) {
	_, err = s.db.Exec(`update or abort pings (url, touched) values (?, now());`, url)
	return
}

func (s *store) Del(url string) (err error) {
	_, err = s.db.Exec(`delete from pings where url=?;`, url)
	return
}

func (s *store) Pings() (pings []string, err error) {
	log.Printf("Querying... \n")
	var ping string
	rows, err := s.db.Query(`select url from pings;`)
	defer rows.Close()
	if err != nil {
		return
	}
	for rows.Next() {
		if err = rows.Scan(&ping); err == nil {
			return
		}
		pings = append(pings, ping)
	}
	return
}

func (s *store) run() {
	for {
		select {
		case c := <-s.Cmd:
			switch c.Action {
			case "query":
				pings, err := s.Pings()
				log.Printf("pings: %+v, %s\n", pings, err)
				if err != nil {
					s.log.Error("Could not get pings: %s", err.Error())
				}
				if pings == nil {
					c.Args = []string{}
				} else {
					c.Args = pings
				}
			case "add":
				err := s.Add(c.Args.(string))
				c.Args = err
			case "ack":
				err := s.Ack(c.Args.(string))
				c.Args = err
			case "del":
				err := s.Del(c.Args.(string))
				c.Args = err
			}
			s.Cmd <- c
		}
	}
}

type hub struct {
	connections map[*connection]bool
	pings       map[string]string
	broadcast   chan *command
	register    chan *connection
	unregister  chan *connection
	cmd         chan *command
	log         *logWriter
	s           *store
}

var h = hub{
	broadcast:   make(chan *command),
	cmd:         make(chan *command),
	register:    make(chan *connection),
	unregister:  make(chan *connection),
	connections: make(map[*connection]bool),
}

func (h *hub) Count() int {
	return len(h.pings)
}

func (h *hub) Proxy(action, pushurl string) (err error) {
	h.s.Cmd <- &command{Action: action, Args: pushurl}
	rep := <-h.s.Cmd
	return rep.Args.(error)
}

func (h *hub) Broadcast(cmd *command) {
	b, _ := json.Marshal(cmd)
	h.broadcast <- &command{Action: "alert", Args: b}
}

func (h *hub) run(st *store) {
	h.s = st
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
				close(c.chat)
			}
		case m := <-h.broadcast:
			h.log.Log("Broadcasting...")
			for c := range h.connections {
				c.chat <- m
			}
		case c := <-h.cmd:
			h.log.Log("proxying client command %+v", *c)
			h.s.Cmd <- c
			r := <-h.s.Cmd
			h.cmd <- r
		}
	}
}

type connection struct {
	ws   *websocket.Conn
	chat chan *command
	cmd  chan *command
	log  *logWriter
}

func (c *connection) Reply(cmd *command) {
	rep, err := json.Marshal(cmd)
	if err == nil {
		c.ws.WriteMessage(websocket.TextMessage, rep)
		return
	}
	c.log.Error("Could not write message: %s", err.Error())
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

		cmd := &command{}
		if err := json.Unmarshal(raw, &cmd); err != nil {
			c.log.Error("Could not process command %s", string(raw))
			return
		}
		switch strings.ToLower(cmd.Action) {
		case "hello":
			// hello command
			c.cmd <- &command{Action: "query"}
			repl := <-c.cmd
			var pings []string
			var count int
			if repl.Args != nil {
				pings = repl.Args.([]string)
				count = len(pings)
			}
			log.Printf("reply hello: %+v", repl)
			c.Reply(&command{Action: "hello",
				Args: struct {
					Clients []string
					Count   int
				}{pings, count}})
		case "interval":
			// TODO:change ping interval

		default:
			c.log.Error("Unknown Command sent from client %+v", cmd)
		}
	}
}

func (c *connection) writer() {
	for chat := range c.chat {
		out, _ := json.Marshal(chat)
		err := c.ws.WriteMessage(websocket.TextMessage, out)
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
		chat: make(chan *command, CHANNELS),
		cmd:  h.cmd,
		ws:   ws,
		log:  &logger,
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
		Cmd: make(chan *command),
		log: logger,
	}
	if err = st.init(); err != nil {
		log.Fatal("Could not create storage: %s", err.Error())
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
		t, err := template.ParseFiles(filepath.Join(*templatePath, "index.html"))
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

	http.HandleFunc("/ack", func(resp http.ResponseWriter, req *http.Request) {
		resp.Header.Add("Access-Control-Allow-Origin", "*")
		var action = "ack"
		if req.Method == "DELETE" {
			action = "del"
		}
		if req.Method == "OPTIONS" {
			http.Error(resp, "", 200)
			return
		}
		pingUrl := req.PostFormValue("sp")
		if pingUrl == "" {
			logger.Error(`Please POST the simplepush url as "sp"`)
			http.Error(resp, `Missing "sp" url value`, 400)
			return
		}
		if err := h.Proxy(action, pingUrl); err != nil {
			logger.Error("Could not handle ping url: %s", err.Error())
			http.Error(resp, `Could not handle ping.`, 500)
			h.Broadcast(&command{Action: "error",
				Args: "Could not handle ping"})
		}
	})

	http.HandleFunc("/reg", func(resp http.ResponseWriter, req *http.Request) {
		resp.Header.Add("Access-Control-Allow-Origin", "*")
		if req.Method != "POST" || req.Method != "OPTIONS" {
			http.Error(resp, "Use POST", 405)
			return
		}
		pingUrl := req.PostFormValue("sp")
		if pingUrl == "" {
			logger.Error(`Please POST the simplepush url as "sp"`)
			http.Error(resp, `Missing "sp" url value`, 400)
			return
		}
		if err := h.Proxy("add", pingUrl); err != nil {
			logger.Error("Could not add ping url: %s", err.Error())
			http.Error(resp, `Could not add ping.`, 500)
			h.Broadcast(&command{Action: "error",
				Args: "Could not add ping"})
		}
	})

	http.Handle("/s/",
		http.StripPrefix("/s/",
			http.FileServer(http.Dir("static"))))

	// Socket for page updates.
	http.HandleFunc("/ws", wsHandler)

	logger.Log("Staring up server at %s\n", *localaddr)
	if err := http.ListenAndServe(*localaddr, nil); err != nil {
		logger.Error("Server failed: %s", err.Error())
	}
}
