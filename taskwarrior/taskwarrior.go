package taskwarrior

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strconv"
	"time"

	"github.com/manishrjain/asanawarrior/x"
)

const (
	stamp = "20060102T150405Z"
)

type task struct {
	Description string   `json:"description"`
	Modified    string   `json:"modified"`
	Status      string   `json:"status"`
	Tags        []string `json:"tags"`
	Project     string   `json:"project"`
	Xid         string   `json:"xid"`
}

func getTasks() ([]task, error) {
	cmd := exec.Command("task", "export")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return nil, err
	}

	var tasks []task
	if err := json.Unmarshal(out.Bytes(), &tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

func GetTasks() ([]x.WarriorTask, error) {
	tasks, err := getTasks()
	if err != nil {
		return nil, err
	}

	wtasks := make([]x.WarriorTask, 0, 100)
	for _, t := range tasks {
		ts, err := time.Parse(stamp, t.Modified)
		if err != nil {
			return nil, err
		}

		var ass, sec string
		var tags []string
		for _, tg := range t.Tags {
			if len(tg) == 0 {
				continue
			}
			switch tg[0] {
			case '@':
				ass = tg[1:]
			case '#':
				sec = tg[1:]
			default:
				tags = append(tags, tg)
			}
		}

		xid, err := strconv.ParseUint(t.Xid, 10, 64)
		if err != nil {
			xid = 0
		}

		wt := x.WarriorTask{
			Assignee:  ass,
			Completed: t.Status == "completed",
			Modified:  ts,
			Name:      t.Description,
			Project:   t.Project,
			Section:   sec,
			Tags:      tags,
			Xid:       xid,
		}
		wtasks = append(wtasks, wt)
	}
	return wtasks, nil
}
