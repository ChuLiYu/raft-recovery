package jobmanager

import (
	"fmt"
	"sort"

	"github.com/ChuLiYu/raft-recovery/pkg/types"
)

// ApplyEnqueue uses only the leader-supplied timestamp in the command.
// A duplicate ID remains a no-op even when its original job was completed.
func (jm *JobManager) ApplyEnqueue(job types.Job) error {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	if job.ID == "" {
		return fmt.Errorf("empty job ID")
	}
	if _, ok := jm.jobs[job.ID]; ok {
		return nil
	}
	copyJob := job
	copyJob.Status = types.StatusPending
	copyJob.Attempt = 0
	copyJob.Deadline = nil
	copyJob.WorkerID = ""
	jm.jobs[job.ID] = &copyJob
	jm.queue = append(jm.queue, job.ID)
	return nil
}

// Pending returns copies in exact FIFO order. Reads never reserve or mutate jobs.
func (jm *JobManager) Pending(limit int) []types.Job {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	if limit <= 0 {
		return nil
	}
	if limit > len(jm.queue) {
		limit = len(jm.queue)
	}
	out := make([]types.Job, 0, limit)
	for _, id := range jm.queue[:limit] {
		if job := jm.jobs[id]; job != nil && job.Status == types.StatusPending {
			out = append(out, *job)
		}
	}
	return out
}

// ApplyDispatch accepts only the current head and next attempt. This fences
// concurrent polls whose proposals named the same pending job.
func (jm *JobManager) ApplyDispatch(id types.JobID, workerID string, attempt int, at, deadline int64) (types.Job, bool, error) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	if workerID == "" || deadline <= at {
		return types.Job{}, false, fmt.Errorf("invalid dispatch worker or deadline")
	}
	job := jm.jobs[id]
	if job == nil || job.Status != types.StatusPending || len(jm.queue) == 0 || jm.queue[0] != id || attempt != job.Attempt+1 {
		return types.Job{}, false, nil
	}
	jm.queue = jm.queue[1:]
	job.Status = types.StatusInFlight
	job.Attempt = attempt
	job.WorkerID = workerID
	job.Deadline = &deadline
	job.UpdatedAt = at
	jm.inFlight[id] = job
	return *job, true, nil
}

// ApplyAck accepts only the currently dispatched worker and attempt.
func (jm *JobManager) ApplyAck(id types.JobID, workerID string, attempt int, status types.JobStatus, at int64, maxRetry int) (bool, error) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	if status != types.StatusCompleted && status != types.StatusDead {
		return false, fmt.Errorf("invalid ACK status %q", status)
	}
	job := jm.jobs[id]
	if job != nil && job.Status == types.StatusCompleted && status == types.StatusCompleted && job.Attempt == attempt && workerID != "" && job.WorkerID == workerID {
		return true, nil
	}
	if job == nil || job.Status != types.StatusInFlight || job.Attempt != attempt || workerID == "" || job.WorkerID != workerID || job.Deadline == nil || at >= *job.Deadline {
		return false, nil
	}
	delete(jm.inFlight, id)
	job.Deadline = nil
	job.UpdatedAt = at
	if status == types.StatusCompleted {
		job.Status = types.StatusCompleted
		jm.completed[id] = job
	} else if maxRetry > 0 && attempt < maxRetry {
		job.Status = types.StatusPending
		job.WorkerID = ""
		jm.queue = append(jm.queue, id)
	} else {
		job.Status = types.StatusDead
		job.WorkerID = ""
		jm.dead[id] = job
	}
	return true, nil
}

// ApplyTimeout is fenced by attempt and the recorded deadline. Followers do
// not consult their own clocks; at comes from the committed command.
func (jm *JobManager) ApplyTimeout(id types.JobID, attempt int, at int64, maxRetry int) (bool, error) {
	jm.mu.Lock()
	defer jm.mu.Unlock()
	job := jm.jobs[id]
	if job == nil || job.Status != types.StatusInFlight || job.Attempt != attempt || job.Deadline == nil || *job.Deadline > at {
		return false, nil
	}
	delete(jm.inFlight, id)
	job.Deadline = nil
	job.WorkerID = ""
	job.UpdatedAt = at
	if maxRetry > 0 && attempt < maxRetry {
		job.Status = types.StatusPending
		jm.queue = append(jm.queue, id)
	} else {
		job.Status = types.StatusDead
		jm.dead[id] = job
	}
	return true, nil
}

func (jm *JobManager) Expired(now int64) []types.Job {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	out := make([]types.Job, 0)
	for _, job := range jm.inFlight {
		if job.Deadline != nil && *job.Deadline <= now {
			out = append(out, *job)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (jm *JobManager) JobCopy(id types.JobID) (types.Job, bool) {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	job := jm.jobs[id]
	if job == nil {
		return types.Job{}, false
	}
	return *job, true
}
