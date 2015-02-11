/* This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at http://mozilla.org/MPL/2.0/. */

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

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
	port         = flag.String("port", "8080", "Local server [8080]")
	host         = flag.String("host", "localhost", "Remote address to use [localhost]")
	dbPath       = flag.String("db", fmt.Sprintf("%s.db", PROJECT), "Path to database file [.]")
	templatePath = flag.String("templates", "template", "Path to templates [./template]")
	period       = flag.Int64("period", 30, "Seconds between server pings [30]")
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
	Action string       `json:"action"`
	Args   []*pingReply `json:"arg"`
	Xtra   interface{}  `json:"xtra"`
	Error  string
}

type pingReply struct {
	URL    string
	Pinged int64
	State  string
	Name   string
}

const STORE_INSERT = `insert or abort into pings (url, name, pinged, state) values (?, ?, strftime('%s', 'now'), 'new');`
const STORE_CREATE = `create table if not exists pings (url string primary key,name string, version integer, pinged integer, state string);`
const STORE_UPDATE = `update pings set state=? where url=?;`
const STORE_DELETE = `delete from pings where url=?;`
const STORE_QUERY = `select url,name, pinged, state from pings;`

//===
type store struct {
	log *logWriter
	db  *sql.DB
	Cmd chan *command
}

func (s *store) init() (err error) {
	_, err = s.db.Exec(STORE_CREATE)
	return
}

func (s *store) Add(info *pingReply) (err error) {
	_, err = s.db.Exec(STORE_INSERT, info.URL, info.Name)
	if err != nil {
		s.log.Error("Add %s", err.Error())
	}
	return
}

func (s *store) upd(state, url string) (err error) {
	_, err = s.db.Exec(STORE_UPDATE, state, url)
	if err != nil {
		s.log.Error("Upd %s", err.Error())
	}
	return
}

func (s *store) Del(url string) (err error) {
	_, err = s.db.Exec(STORE_DELETE, url)
	if err != nil {
		s.log.Error("Del %s", err.Error())
	}
	return
}

func (s *store) Hello() (pings []*pingReply, err error) {
	pings = []*pingReply{}
	rows, err := s.db.Query(STORE_QUERY)
	defer rows.Close()
	if err != nil {
		s.log.Error("Pings: %s", err.Error())
		return
	}
	for rows.Next() {
		pr := pingReply{}
		if err = rows.Scan(&pr.URL, &pr.Name, &pr.Pinged, &pr.State); err != nil {
			s.log.Error("Pings: %s", err.Error())
			return
		}
		pings = append(pings, &pr)
	}
	return
}

func (s *store) Pings() (pings []*pingReply, err error) {
	pings, err = s.Hello()
	if err != nil {
		return
	}
	body := bytes.NewBufferString(fmt.Sprintf("version=%d", time.Now().UTC().Unix()))
	for _, pr := range pings {
		if pr.State == "ping" {
			pr.State = "offline"
		} else {
			pr.State = "ping"
		}
		err := s.ping(pr.URL, body)
		if err != nil {
			s.log.Error("Pings %s", err.Error())
		}
	}
	return
}

func (s *store) ping(url string, body io.Reader) (err error) {
	client := &http.Client{}
	req, err := http.NewRequest("PUT", url, body)
	if err != nil {
		s.log.Error("ping %s", err)
		return
	}
	_, err = client.Do(req)
	if err == nil {
		s.log.Log("updating %s\n", url)
		err = s.upd("ping", url)
	} else {
		s.log.Error("Failed to push %s", err.Error())
	}
	return
}

func (s *store) run() {
	for {
		select {
		case c := <-s.Cmd:
			switch c.Action {
			case "hello":
				pings, err := s.Hello()
				if err != nil {
					s.log.Error("Could not get pings: %s", err.Error())
					continue
				}
				if pings == nil {
					c.Args = []*pingReply{&pingReply{}}
				} else {
					c.Args = pings
				}
			case "add":
				if err := s.Add(c.Args[0]); err != nil {
					s.log.Error("Could not add url %s", err.Error())
					c.Error = err.Error()
				}
			case "ack":
				if err := s.upd("ack", c.Args[0].URL); err != nil {
					c.Error = err.Error()
				}
			case "del":
				if err := s.Del(c.Args[0].URL); err != nil {
					c.Error = err.Error()
				}
			case "ping":
				pings, err := s.Pings()
				if err == nil {
					if pings == nil {
						c.Args = []*pingReply{&pingReply{}}
					} else {
						c.Args = pings
					}
				} else {
					c.Error = err.Error()
				}
				c.Xtra = struct{ Period int64 }{*period}
			}
			s.Cmd <- c
		}
	}
}

//===
type hub struct {
	connections map[*connection]bool
	pings       map[string]string
	broadcast   chan *command
	register    chan *connection
	unregister  chan *connection
	cmd         chan *command
	log         *logWriter
	s           *store
	period      int64
}

var h = hub{
	broadcast:   make(chan *command),
	cmd:         make(chan *command),
	register:    make(chan *connection),
	unregister:  make(chan *connection),
	connections: make(map[*connection]bool),
}

func (h *hub) Proxy(action string, ci *pingReply) (rep *command) {
	h.s.Cmd <- &command{Action: action, Args: []*pingReply{ci}}
	return <-h.s.Cmd
}

func (h *hub) Broadcast(cmd *command) {
	h.broadcast <- cmd
}

func (h *hub) pinger() {
	rep := h.Proxy("ping", &pingReply{})
	h.broadcast <- rep
}

func (h *hub) run(st *store, period *int64) {
	h.s = st

	go func(period *int64) {
		for {
			select {
			case <-time.After(time.Second * time.Duration(*period)):
				h.pinger()
			}
		}
	}(period)

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
			// do this directly in case there's a bunch of content in Args
			h.s.Cmd <- c
			r := <-h.s.Cmd
			h.cmd <- r
		}
	}
}

// ===
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
		case "hello", "ping":
			c.cmd <- cmd
			repl := <-c.cmd
			c.Reply(repl)
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
	go h.run(st, period)

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
		wsHost := fmt.Sprintf("%s:%s", *host, *port)
		err = t.Execute(resp, struct {
			Host   string
			Period int64
		}{Host: wsHost, Period: *period})
		if err != nil {
			fatal(resp, err)
		}
		return
	})

	// Push Client ACK handler
	http.HandleFunc("/ack", func(resp http.ResponseWriter, req *http.Request) {
		resp.Header().Add("Access-Control-Allow-Origin", "*")
		var action = "ack"
		// future use.
		if req.Method == "DELETE" {
			action = "del"
		}
		if req.Method == "OPTIONS" {
			http.Error(resp, "", 200)
			return
		}
		pingUrl := req.PostFormValue("sp")
		name := req.PostFormValue("name")
		if pingUrl == "" {
			logger.Error(`Please POST the simplepush url as "sp"`)
			http.Error(resp, `Missing "sp" url value`, 400)
			return
		}
		logger.Log("Sending ack for %s", name)
		rep := h.Proxy(action, &pingReply{URL: pingUrl,
			Name: name, State: "ack"})
		if rep.Error != "" {
			logger.Error("Could not add ping url: %s", rep.Error)
			http.Error(resp, `Could not add ping.`, 500)
			rep.Action = "error"
			h.Broadcast(rep)
			return
		}
		h.Broadcast(rep)
	})

	// Push Client Registration callback
	http.HandleFunc("/reg", func(resp http.ResponseWriter, req *http.Request) {
		resp.Header().Add("Access-Control-Allow-Origin", "*")
		if req.Method == "OPTIONS" {
			return
		}
		if req.Method != "POST" {
			http.Error(resp, fmt.Sprintf("Use POST: %s", req.Method), 405)
			return
		}
		pingUrl := req.FormValue("sp")
		name := req.FormValue("name")
		if pingUrl == "" {
			logger.Error(`Please POST the simplepush url as "sp"`)
			http.Error(resp, `Missing "sp" url value`, 400)
			return
		}
		logger.Log("Trying %s", pingUrl)
		rep := h.Proxy("add", &pingReply{URL: pingUrl, Name: name})
		if rep.Error != "" {
			logger.Error("Could not add ping url: %s", rep.Error)
			http.Error(resp, `Could not add ping.`, 500)
			rep.Action = "error"
			h.Broadcast(rep)
			return
		}
		h.Broadcast(rep)
	})

	// Static content handler
	http.Handle("/s/",
		http.StripPrefix("/s/",
			http.FileServer(http.Dir("static"))))

	// Socket for page updates.
	http.HandleFunc("/ws", wsHandler)

	logger.Log("Staring up server at %s\n", fmt.Sprintf(":%s", *port))
	if err := http.ListenAndServe(fmt.Sprintf(":%s", *port), nil); err != nil {
		logger.Error("Server failed: %s", err.Error())
	}
}
