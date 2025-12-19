package raft

import (
	"encoding/json"
	"github.com/ChuLiYu/raft-recovery/pkg/types"
)

// CommandType identifies the type of raft command
type CommandType string

const (
	CmdEnqueue CommandType = "ENQUEUE"
	CmdAck     CommandType = "ACK"
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
	JobID  string          `json:"job_id"`
	Status types.JobStatus `json:"status"`
	// Result could be added here
}

// NewEnqueueCommand creates an encoded Enqueue command
func NewEnqueueCommand(jobs []types.Job) ([]byte, error) {
	payload, _ := json.Marshal(EnqueuePayload{Jobs: jobs})
	cmd := RaftCommand{
		Type:    CmdEnqueue,
		Payload: payload,
	}
	return json.Marshal(cmd)
}

// NewAckCommand creates an encoded Ack command
func NewAckCommand(jobID string, status types.JobStatus) ([]byte, error) {
	payload, _ := json.Marshal(AckPayload{JobID: jobID, Status: status})
	cmd := RaftCommand{
		Type:    CmdAck,
		Payload: payload,
	}
	return json.Marshal(cmd)
}
