package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

type Event interface {
	Format() []byte
	Broadcast() bool
}

type Login struct {
	Nick string
}

func (j *Login) Format() []byte {
	return []byte(fmt.Sprintf("Login %s\n", j.Nick))
}

func (j *Login) Broadcast() bool {
	return false
}

type PublicMessage struct {
	Author  string
	Message string
}

func (pm *PublicMessage) Format() []byte {
	return []byte(fmt.Sprintf("%s: %s\n", pm.Author, pm.Message))
}

func (pm *PublicMessage) Broadcast() bool {
	return true
}

type PrivateMessage struct {
	Author    string
	Recipient string
	Message   string
}

func (privmsg *PrivateMessage) Format() []byte {
	return []byte(fmt.Sprintf("P %s: %s\n", privmsg.Author, privmsg.Recipient))
}

func (privmsg *PrivateMessage) Broadcast() bool {
	return true
}

func parseEvent(line string, author string) Event {
	split := strings.Split(line, " ")
	switch split[0] {
	case "Login":
		return &Login{Nick: strings.Trim(split[1], "\n")}
	case "Privmsg":
		return &PrivateMessage{
			Author:    author,
			Recipient: split[1],
			Message:   strings.Join(split[2:], " "),
		}
	case "Pubmsg":
		return &PublicMessage{
			Author:  author,
			Message: strings.Join(split[1:], " "),
		}
	}
	return nil
}

type Client struct {
	conn       net.Conn
	nick       string
	mutex      sync.RWMutex
	customNick bool
}

type Server struct {
	clients map[string]*Client
	mutex   sync.RWMutex
}

func newServer() Server {
	return Server{
		clients: make(map[string]*Client),
		mutex:   sync.RWMutex{},
	}
}

func (c *Client) changeNickname(newNick string) {
	if c.customNick {
		return
	}

	c.mutex.Lock()
	c.nick = newNick
	c.customNick = true
	c.mutex.Unlock()
}

func (s *Server) addClient(clientId string, conn net.Conn) *Client {
	s.mutex.Lock()

	client := &Client{
		conn:       conn,
		nick:       fmt.Sprintf("%x", sha256.Sum256([]byte(clientId))),
		mutex:      sync.RWMutex{},
		customNick: false,
	}

	s.clients[clientId] = client

	s.mutex.Unlock()

	return client
}

func (s *Server) removeClient(clientId string) {
	s.mutex.Lock()

	delete(s.clients, clientId)

	s.mutex.Unlock()
}

func (s *Server) broadcast(events <-chan Event, ctx context.Context) {
	for {
		select {
		case event := <-events:
			if !event.Broadcast() {
				continue
			}
			formatted := event.Format()
			s.mutex.RLock()
			if priv, ok := event.(*PrivateMessage); ok {
				for _, client := range s.clients {
					if client.nick == priv.Recipient {
						client.conn.Write(formatted)
						break
					}
				}
			} else {
				for _, client := range s.clients {
					client.conn.Write(formatted)
				}
			}
			s.mutex.RUnlock()
		case <-ctx.Done():
			fmt.Println("Stopping broadcast")
			return
		}
	}
}

func (s *Server) handleClient(conn net.Conn, events chan<- Event) {
	defer conn.Close()

	address := conn.RemoteAddr().String()
	log.Println("Accepted new client with ip:", address)

	clientId := fmt.Sprintf("%s-%d", address, time.Now().Unix())

	client := s.addClient(clientId, conn)
	defer s.removeClient(clientId)

	for {
		line, err := bufio.NewReader(conn).ReadString('\n')
		if err != nil {
			if err.Error() != "EOF" {
				log.Printf("Error retrieving message for socket with ip %s: %v\n", address, err)
			}
			break
		}

		event := parseEvent(line, client.nick)

		if event == nil {
			continue
		}

		if login, ok := event.(*Login); ok {
			client.changeNickname(login.Nick)
		}

		events <- event
	}
}

func main() {
	listen, err := net.Listen("tcp", "127.0.0.1:6001")
	log.Println("Server started")

	if err != nil {
		panic(err)
	}

	defer listen.Close()

	events := make(chan Event, 32)
	ctx, cancel := context.WithCancel(context.Background())

	defer cancel()
	defer close(events)

	server := newServer()

	go server.broadcast(events, ctx)

	for {
		conn, err := listen.Accept()
		if err != nil {
			log.Printf("Error accepting connection: %v\n", err)
			continue
		}

		go server.handleClient(conn, events)
	}
}
