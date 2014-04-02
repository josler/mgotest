// Package mgotest provides standalone test instances of mongo sutable for use
// in tests.
package mgotest

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"text/template"
	"time"

	"labix.org/v2/mgo"

	"github.com/facebookgo/freeport"
	"github.com/facebookgo/waitout"
)

var mgoWaitingForConnections = []byte("waiting for connections on port")

var configTemplate, configTemplateErr = template.New("config").Parse(`
bind_ip          = 127.0.0.1
dbpath           = {{.DBPath}}
nohttpinterface  = true
nojournal        = true
noprealloc       = true
nounixsocket     = true
nssize           = 2
port             = {{.Port}}
quiet            = true
smallfiles       = true
{{if .ReplSet}}
oplogSize        = 1
replSet          = rs
{{end}}
`)

func init() {
	if configTemplateErr != nil {
		panic(configTemplateErr)
	}
}

// Fatalf is satisfied by testing.T or testing.B.
type Fatalf interface {
	Fatalf(format string, args ...interface{})
}

// Server is a unique instance of a mongod.
type Server struct {
	Port        int
	DBPath      string
	ReplSet     bool
	StopTimeout time.Duration
	T           Fatalf
	cmd         *exec.Cmd
}

// Start the server, this will return once the server has been started.
func (s *Server) Start() {
	if s.Port == 0 {
		port, err := freeport.Get()
		if err != nil {
			s.T.Fatalf(err.Error())
		}
		s.Port = port
	}

	dir, err := ioutil.TempDir("", "mgotest-dbpath-")
	if err != nil {
		s.T.Fatalf(err.Error())
	}
	s.DBPath = dir

	cf, err := ioutil.TempFile(s.DBPath, "config-")
	if err != nil {
		s.T.Fatalf(err.Error())
	}
	if err := configTemplate.Execute(cf, s); err != nil {
		s.T.Fatalf(err.Error())
	}
	if err := cf.Close(); err != nil {
		s.T.Fatalf(err.Error())
	}

	waiter := waitout.New(mgoWaitingForConnections)
	s.cmd = exec.Command("mongod", "--config", cf.Name())
	s.cmd.Env = envPlusLcAll()
	if os.Getenv("MGOTEST_VERBOSE") == "1" {
		s.cmd.Stdout = io.MultiWriter(os.Stdout, waiter)
		s.cmd.Stderr = os.Stderr
	} else {
		s.cmd.Stdout = waiter
	}
	if err := s.cmd.Start(); err != nil {
		s.T.Fatalf(err.Error())
	}
	waiter.Wait()
}

// Stop the server, this will also remove all data.
func (s *Server) Stop() {
	fin := make(chan struct{})
	go func() {
		defer close(fin)
		s.cmd.Process.Kill()
		os.RemoveAll(s.DBPath)
	}()
	select {
	case <-fin:
	case <-time.After(s.StopTimeout):
	}
}

// URL for the mongo server, suitable for use with mgo.Dial.
func (s *Server) URL() string {
	return fmt.Sprintf("127.0.0.1:%d", s.Port)
}

// Session for the mongo server.
func (s *Server) Session() *mgo.Session {
	session, err := mgo.Dial(s.URL())
	if err != nil {
		s.T.Fatalf(err.Error())
	}
	return session
}

// NewStartedServer creates a new server starts it.
func NewStartedServer(t Fatalf) *Server {
	s := &Server{
		T:           t,
		StopTimeout: 15 * time.Second,
	}
	s.Start()
	return s
}

// NewReplSetServer creates a new server starts it with ReplSet enabled.
func NewReplSetServer(t Fatalf) *Server {
	s := &Server{
		T:           t,
		StopTimeout: 15 * time.Second,
		ReplSet:     true,
	}
	s.Start()
	return s
}

func envPlusLcAll() []string {
	env := os.Environ()
	return append(env, "LC_ALL=C")
}
