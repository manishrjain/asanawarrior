package x

import "time"

type WarriorTask struct {
	Assignee  string
	Completed bool
	Modified  time.Time
	Name      string
	Project   string
	Section   string
	Tags      []string
	Xid       uint64
}
