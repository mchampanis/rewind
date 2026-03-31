package ipc

import (
	"encoding/json"
	"fmt"
)

// Command is a JSON request sent to the daemon over the named pipe.
type Command struct {
	Name string   `json:"cmd"`
	Args []string `json:"args,omitempty"`
}

// Response is a JSON reply from the daemon.
type Response struct {
	OK    bool           `json:"ok"`
	Error string         `json:"error,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
}

func (r Response) String() string {
	if !r.OK {
		return fmt.Sprintf("error: %s", r.Error)
	}
	if len(r.Data) == 0 {
		return "ok"
	}
	b, err := json.MarshalIndent(r.Data, "", "  ")
	if err != nil {
		return fmt.Sprintf("error: failed to format response: %v", err)
	}
	return string(b)
}

func (r Response) Encode() ([]byte, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

func DecodeCommand(b []byte) (Command, error) {
	var c Command
	return c, json.Unmarshal(b, &c)
}

func DecodeResponse(b []byte) (Response, error) {
	var r Response
	return r, json.Unmarshal(b, &r)
}
