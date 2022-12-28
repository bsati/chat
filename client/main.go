package main

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"

	go_ds "github.com/bsati/go-ds"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// selector serves as a generic bubbletea model
// for events that require one or multiple selections
type selector struct {
	choices  []string
	cursor   int
	selected go_ds.Set[int]
}

type selectionEvent = int

func NewSelectorModel(choices []string) selector {
	return selector{
		choices:  choices,
		cursor:   0,
		selected: go_ds.NewSet[int](),
	}
}

func (s selector) Init() tea.Cmd {
	return nil
}

func (s selector) Update(msg tea.Msg) (selector, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		switch msg.String() {

		case "ctrl+c", "q":
			return s, tea.Quit

		case "up":
			if s.cursor > 0 {
				s.cursor--
			}

		case "down":
			if s.cursor < len(s.choices)-1 {
				s.cursor++
			}

		case "enter", " ":
			if !s.selected.Add(s.cursor) {
				s.selected.Remove(s.cursor)
			}
		}
	}
	return s, nil
}

func (s selector) View() string {
	headline := "Select a server to join:\n\n"

	for i, choice := range s.choices {
		cursor := " "
		if s.cursor == i {
			cursor = ">"
		}

		headline += fmt.Sprintf("%s %s\n", cursor, choice)
	}

	return headline
}

// model represents the whole application state
// for the bubbletea program
type model struct {
	connection     *net.TCPConn
	messages       []string
	viewport       viewport.Model
	input          textinput.Model
	serverSelector selector
}

func initialModel() model {
	ti := textinput.New()
	ti.Placeholder = "Write a message..."
	ti.CharLimit = 255
	ti.Width = 45
	ti.Prompt = "| "

	vp := viewport.New(45, 5)
	vp.SetContent("Type a message to join the conversation!")

	s := NewSelectorModel([]string{"127.0.0.1:6002", "127.0.0.1:6001"})

	return model{
		input:          ti,
		serverSelector: s,
		viewport:       vp,
		connection:     nil,
		messages:       nil,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func GetSelected[T comparable](set go_ds.Set[T]) T {
	var result T
	if len(set) == 0 {
		return result
	}

	for k := range set {
		return k
	}
	return result
}

type newMessageMsg = string

func (m *model) readSocketMessages() tea.Msg {
	line, err := bufio.NewReader(m.connection).ReadString('\n')
	if err != nil {
		log.Fatal("Error reading from socket: ", err)
	}
	return newMessageMsg(line)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd     tea.Cmd
		vpCmd     tea.Cmd
		sCmd      tea.Cmd
		updateCmd tea.Cmd
	)

	// Depending on the current state (whether a connection has been established or not)
	// different parts of the TUI have to be updated
	if m.connection != nil {
		m.input, tiCmd = m.input.Update(msg)
		m.viewport, vpCmd = m.viewport.Update(msg)
	} else {
		m.serverSelector, sCmd = m.serverSelector.Update(msg)
		if len(m.serverSelector.selected) != 0 {
			addr, _ := net.ResolveTCPAddr("tcp", m.serverSelector.choices[GetSelected(m.serverSelector.selected)])
			conn, err := net.DialTCP("tcp", nil, addr)
			if err != nil {
				panic(fmt.Sprintf("Error connecting to server: %v", err))
			}
			m.connection = conn
			m.input.Focus()
		}
	}

	// Check again if a connection has been established to start the background task
	if m.connection != nil {
		updateCmd = m.readSocketMessages
	}

	// Pre-batch update commands for returns
	batched := tea.Batch(tiCmd, vpCmd, sCmd, updateCmd)

	switch msg := msg.(type) {
	case newMessageMsg:
		// Only update the view if a new non-empty message has been received
		if len(msg) == 0 {
			break
		}
		m.messages = append(m.messages, msg[0:len(msg)-1])
		m.viewport.SetContent(strings.Join(m.messages, "\n"))
		m.viewport.GotoBottom()
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			// gracefully close the connection before exiting
			if m.connection != nil {
				m.connection.Close()
			}
			return m, tea.Quit
		case tea.KeyEnter:
			// only send message when connection has been established
			if m.connection == nil {
				return m, batched
			}
			if m.input.Value() == "" {
				return m, batched
			}
			_, err := m.connection.Write([]byte(m.input.Value() + "\n"))
			if err != nil {
				m.input.SetValue(err.Error())
			} else {
				m.viewport.SetContent(strings.Join(m.messages, "\n"))
				m.viewport.GotoBottom()
				m.input.Reset()
			}
		}
	case error:
		log.Fatal(msg)
		return m, nil
	}

	return m, batched
}

func (m model) View() string {
	if m.connection == nil {
		return m.serverSelector.View()
	}
	return fmt.Sprintf("%s\n\n%s", m.viewport.View(), m.input.View())
}

func main() {
	p := tea.NewProgram(initialModel())

	if _, err := p.Run(); err != nil {
		log.Fatal(err)
	}
}
