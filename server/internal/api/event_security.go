package api

import (
	"net/http"
	"sync"

	"github.com/ai-employee-platform/server/internal/auth"
	"github.com/ai-employee-platform/server/internal/eventbus"
)

var eventStreams = struct {
	sync.Mutex
	users map[string]int
	total int
}{users: map[string]int{}}

func acquireEventStream(user string) bool {
	eventStreams.Lock()
	defer eventStreams.Unlock()
	if eventStreams.total >= 256 || eventStreams.users[user] >= 4 {
		return false
	}
	eventStreams.users[user]++
	eventStreams.total++
	return true
}
func releaseEventStream(user string) {
	eventStreams.Lock()
	defer eventStreams.Unlock()
	eventStreams.users[user]--
	eventStreams.total--
	if eventStreams.users[user] == 0 {
		delete(eventStreams.users, user)
	}
}
func (d Deps) visibleBusEvent(r *http.Request, sess *auth.Session, ev eventbus.Event) bool {
	if id := ev.Payload["job_id"]; id != "" && d.Jobs != nil {
		if d.Auth.Authorize(r.Context(), sess, "job.read") != nil {
			return false
		}
		j, err := d.Jobs.Get(r.Context(), id)
		return err == nil && j != nil && d.canAccessJob(r, sess, j, "job.read")
	}
	if id := ev.Payload["session_id"]; id != "" && d.Sessions != nil {
		if d.Auth.Authorize(r.Context(), sess, "session.read") != nil {
			return false
		}
		s, err := d.Sessions.Get(r.Context(), id)
		return err == nil && s != nil && d.employeeOwnedBy(r, sess, s.EmployeeID, "session.read")
	}
	employee := ev.Payload["employee_id"]
	if ev.Type == eventbus.TypeEmployeeChanged {
		employee = ev.Payload["id"]
	}
	if employee != "" {
		return d.Auth.Authorize(r.Context(), sess, "employee.read") == nil && d.employeeOwnedBy(r, sess, employee, "employee.read")
	}
	if id := ev.Payload["workstation_id"]; id != "" {
		return d.Auth.Authorize(r.Context(), sess, "workstation.read") == nil && (d.scopeAll(r, sess, "workstation.read") || d.workstationCreatedBy(r.Context(), id) == sess.UserID)
	}
	return d.scopeAll(r, sess, "system.read")
}
