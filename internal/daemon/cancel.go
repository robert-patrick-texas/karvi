package daemon

import (
	"encoding/json"
	"log/slog"
	"net"

	"github.com/robert-patrick-texas/karvi/internal/errorcodes"
	"github.com/robert-patrick-texas/karvi/internal/ipc"
)

// cancelJob serves cancel_job: peer UID was checked
// by the caller; the payload is validated, the job looked up in the
// bounded job table (job_unknown otherwise), the request applied through
// the entry, and the result written at once. The request is logged under
// the reason code cancelled with the job ID and the requesting PID.
func (s *Server) cancelJob(conn net.Conn, req ipc.Request) {
	var cr ipc.CancelRequest
	if err := json.Unmarshal(req.Payload, &cr); err != nil {
		s.writeError(conn, "job_request_malformed", err, req.RequestID)
		return
	}
	if err := cr.Validate(); err != nil {
		s.writeCoded(conn, "job_request_malformed", err, req.RequestID)
		return
	}
	j := s.jobTable.get(cr.JobID)
	if j == nil {
		s.logf(req.Operation, req.RequestID, "", cr.JobID, "job_unknown")
		s.writeError(conn, "job_unknown", errorcodes.Errorf("job_unknown", "the daemon holds no job %s", cr.JobID), req.RequestID)
		return
	}
	pid := 0
	if uc, ok := conn.(*net.UnixConn); ok {
		if p, err := ipc.PeerPID(uc); err == nil {
			pid = int(p)
		}
	}
	res, first := j.requestCancel(cr.Reason, req.RequestID, pid, s.UID, s.now())
	s.logf(req.Operation, req.RequestID, "", cr.JobID, "cancelled")
	if first {
		if s.Logger != nil {
			s.Logger.Info("cancel requested", slog.String("job_id", cr.JobID), slog.Int("requester_pid", pid), slog.String("request_id", req.RequestID), slog.String("reason", cr.Reason))
		}
		// The audit record is written once, for the request that cancelled;
		// a repeated request writes nothing.
		if j.auditCancel != nil && j.cancelled != nil {
			j.auditCancel(*j.cancelled)
		}
	}
	s.writeResult(conn, req.RequestID, res)
}
