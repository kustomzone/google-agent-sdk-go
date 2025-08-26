package internal

import (
	"sync"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Client struct {
	conn *websocket.Conn
}

type Port struct {
	Name     string `json:"name,omitempty"`
	StreamID string `json:"streamId,omitempty"`
}

type ActionGraph struct {
	Actions []*Action `json:"actions,omitempty"`
	Outputs []*Port   `json:"outputs,omitempty"`
}

type Action struct {
	Name    string  `json:"name,omitempty"`
	Inputs  []*Port `json:"inputs,omitempty"`
	Outputs []*Port `json:"outputs,omitempty"`
	// TODO: Add configs.
}

type Chunk struct {
	MIMEType string `json:"mimeType,omitempty"`
	Data     []byte `json:"data,omitempty"`
	// TODO: Add metadata.
}

type StreamFrame struct {
	StreamID  string `json:"streamId,omitempty"`
	Data      *Chunk `json:"data,omitempty"`
	Continued bool   `json:"continued,omitempty"`
}

type executeActionsMsg struct {
	SessionID    string         `json:"sessionId,omitempty"`
	ActionGraph  *ActionGraph   `json:"actionGraph,omitempty"`
	StreamFrames []*StreamFrame `json:"streamFrames,omitempty"`
}

func NewClient(endpoint string, apiKey string) (*Client, error) {
	c, _, err := websocket.DefaultDialer.Dial(endpoint+"?key="+apiKey, nil)
	if err != nil {
		return nil, err
	}
	return &Client{conn: c}, nil
}

type Session struct {
	mu        sync.Mutex
	c         *Client
	sessionID string

	pendingWrites map[string]string // stream_id, output_id
}

func (c *Client) OpenSession(sessionID string) (*Session, error) {
	return &Session{
		c:             c,
		sessionID:     sessionID,
		pendingWrites: make(map[string]string),
	}, nil
}

func (s *Session) ID() string {
	return s.sessionID
}

func (s *Session) WriteFrame(id string, c *Chunk, continued bool) error {
	// Don't allow any other activity on the bidi connection
	// until the chunk write is on the wire.
	// TODO: Probably it's better to accept an iterator as input
	// and block WriteFrame until every chunk is written for the stream.
	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.pendingWrites[id]

	if !ok {
		output := uuid.NewString()
		if err := s.c.conn.WriteJSON(&executeActionsMsg{
			SessionID: s.sessionID,
			ActionGraph: &ActionGraph{
				Actions: []*Action{
					{
						Name:   "save_stream",
						Inputs: []*Port{{Name: "input", StreamID: id}},
					},
				},
				Outputs: []*Port{
					{
						Name:     "output",
						StreamID: output,
					},
				},
			},
		}); err != nil {
			return err
		}
		s.pendingWrites[id] = output
	}

	// TODO: Remove the dangling reference in waiting if continued is not true.
	// TODO: Pull the output?
	return s.c.conn.WriteJSON(&executeActionsMsg{
		StreamFrames: []*StreamFrame{{
			StreamID:  id,
			Data:      c,
			Continued: continued,
		}},
	})
}

func (s *Session) ReadAll(id string) ([]*Chunk, error) {
	// Don't allow any other activity on the bidi connection
	// until the read is completed.
	s.mu.Lock()
	defer s.mu.Unlock()

	var chunks []*Chunk
	if err := s.c.conn.WriteJSON(&executeActionsMsg{
		SessionID: s.sessionID,
		ActionGraph: &ActionGraph{
			Actions: []*Action{
				{
					Name:    "restore_stream",
					Outputs: []*Port{{Name: "output", StreamID: id}},
				},
			},
			Outputs: []*Port{{Name: "output", StreamID: id}},
		},
	}); err != nil {
		return nil, err
	}
	for {
		var resp executeActionsMsg
		if err := s.c.conn.ReadJSON(&resp); err != nil {
			return nil, err
		}
		for _, frame := range resp.StreamFrames {
			chunks = append(chunks, frame.Data)
			if !frame.Continued {
				return chunks, nil
			}
		}
	}
}
