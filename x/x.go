package x

import "time"

type WarriorTask struct {
	Assignee  string
	Completed time.Time
	Created   time.Time
	Modified  time.Time
	Name      string
	Project   string
	Section   string
	Tags      []string
	Xid       uint64
	Uuid      string

	// TaskWarrior
	Deleted bool
}
