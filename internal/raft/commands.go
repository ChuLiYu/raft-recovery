package raft

import (
	"encoding/json"

	"github.com/ChuLiYu/raft-recovery/pkg/types"
)

// CommandType identifies the type of raft command
type CommandType string

const (
	CmdEnqueue  CommandType = "ENQUEUE"
	CmdDispatch CommandType = "DISPATCH"
	CmdAck      CommandType = "ACK"
	CmdTimeout  CommandType = "TIMEOUT"
)

// RaftCommand is the data structure serialized into the Raft log
type RaftCommand struct {
	Type    CommandType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// EnqueuePayload is the payload for ENQUEUE command
type EnqueuePayload struct {
	Jobs []types.Job `json:"jobs"`
}

// AckPayload is the payload for ACK command
type AckPayload struct {
	JobID    string          `json:"job_id"`
	Status   types.JobStatus `json:"status"`
	Attempt  int             `json:"attempt"`
	WorkerID string          `json:"worker_id"`
	At       int64           `json:"at_ms"`
	MaxRetry int             `json:"max_retry"`
	ResultID string          `json:"result_id,omitempty"`
}

type DispatchPayload struct {
	JobID    string `json:"job_id"`
	WorkerID string `json:"worker_id"`
	Attempt  int    `json:"attempt"`
	At       int64  `json:"at_ms"`
	Deadline int64  `json:"deadline_ms"`
	ResultID string `json:"result_id,omitempty"`
}

type TimeoutPayload struct {
	JobID    string `json:"job_id"`
	Attempt  int    `json:"attempt"`
	At       int64  `json:"at_ms"`
	MaxRetry int    `json:"max_retry"`
}

// NewEnqueueCommand creates an encoded Enqueue command
func NewEnqueueCommand(jobs []types.Job) ([]byte, error) {
	payload, err := json.Marshal(EnqueuePayload{Jobs: jobs})
	if err != nil {
		return nil, err
	}
	cmd := RaftCommand{
		Type:    CmdEnqueue,
		Payload: payload,
	}
	return json.Marshal(cmd)
}

// NewAckCommand creates an encoded Ack command
func NewAckCommand(jobID string, status types.JobStatus, attempt ...int) ([]byte, error) {
	a := 0
	if len(attempt) > 0 {
		a = attempt[0]
	}
	payload, err := json.Marshal(AckPayload{JobID: jobID, Status: status, Attempt: a})
	if err != nil {
		return nil, err
	}
	cmd := RaftCommand{
		Type:    CmdAck,
		Payload: payload,
	}
	return json.Marshal(cmd)
}

func NewAckAttemptCommand(payload AckPayload) ([]byte, error) {
	p, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(RaftCommand{Type: CmdAck, Payload: p})
}

func NewDispatchCommand(payload DispatchPayload) ([]byte, error) {
	p, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(RaftCommand{Type: CmdDispatch, Payload: p})
}

func NewTimeoutCommand(payload TimeoutPayload) ([]byte, error) {
	p, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(RaftCommand{Type: CmdTimeout, Payload: p})
}
