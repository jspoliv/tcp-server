package main

import (
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
)

// Message received in a Read() loop
type Message struct {
	data []byte // value received from a buffer
	from string // stringfied address from the sender
}

// Peer is a group of connections with channels for adding/removing connections
type Peer struct {
	list map[net.Conn]struct{}
	add  chan net.Conn
	del  chan net.Conn
}

// Returns an initialized instance of *Peer
func NewPeer() *Peer {
	return &Peer{
		list: make(map[net.Conn]struct{}),
		add:  make(chan net.Conn),
		del:  make(chan net.Conn),
	}
}

// Server that handles messages for a group of connections
type Server struct {
	ln       net.Listener
	peers    *Peer
	msg      chan Message
	shutdown chan os.Signal
}

// Returns an initialized instance of *Server
// sets up a signal for shutdown
func NewServer() (s *Server) {
	s = &Server{
		peers:    NewPeer(),
		msg:      make(chan Message),
		shutdown: make(chan os.Signal),
	}
	signal.Notify(s.shutdown, syscall.SIGINT, syscall.SIGTERM)
	return
}

// Starts the server listening, the selectLoop goroutine and an acceptLoop
func (s *Server) Start(address string) error {
	ln, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	s.ln = ln

	go s.selectLoop()

	slog.Info("tcp server started", "port", address)

	if err := s.acceptLoop(); err != nil {
		return err
	}
	return nil
}

// Accepts and handles incoming connections in a loop
func (s *Server) acceptLoop() error {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			fmt.Println("err =>", err.Error())
			continue
		}
		go s.handleConnection(conn)
	}
}

// Handles receiving channels
// All channels are unbuffered so cases block each other
func (s *Server) selectLoop() {
	for {
		select {
		case conn := <-s.peers.del: // removes conn from peers.list
			slog.Info("peer disconnected", "addr", conn.RemoteAddr())
			delete(s.peers.list, conn)
			conn.Close()
		case msg := <-s.msg: // receives and handles a Message
			if err := s.handleMessage(msg); err != nil {
				fmt.Println(err)
			}
		case conn := <-s.peers.add: // adds an incoming connection to peers.list, starts a read loop for that connection
			slog.Info("new peer connected", "addr", conn.RemoteAddr())
			s.peers.list[conn] = struct{}{}
			go s.readMsgLoop(conn)
		case signal := <-s.shutdown: // on interrupt: closes the connections in peers.list, closes the server listener
			for peer := range s.peers.list {
				peer.Close()
			}
			fmt.Printf("%v: gracefull server shutdown\n", signal)
			s.ln.Close()
		}
	}
}

// Reads incoming messages in a loop
// Sends a Message through the s.msg channel when successful
// Sends a net.Conn through the s.peers.del channel when Read(buf) errs, then returns
func (s *Server) readMsgLoop(conn net.Conn) {
	buf := make([]byte, 1024)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			fmt.Println("read() error:", err)
			s.peers.del <- conn
			return
		}
		s.msg <- Message{
			data: buf[:n],
			from: conn.RemoteAddr().String(),
		}
	}
}

// Resends the incoming Message to the other connections in s.peers.list
func (s *Server) handleMessage(msg Message) error {
	fmt.Printf("%s\n", string(msg.data))
	for peer := range s.peers.list {
		if peer.RemoteAddr().String() != msg.from {
			peer.Write(msg.data)
		}
	}
	return nil
}

// Sends conn through the s.peers.add channel
func (s *Server) handleConnection(conn net.Conn) {
	s.peers.add <- conn
	fmt.Printf("handling connection addr=%v\n", conn.RemoteAddr())
}

func main() {
	s := NewServer()
	if err := s.Start(":3000"); err != nil {
		log.Fatal(err)
	}
}
