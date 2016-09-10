package taskwarrior

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"time"

	"github.com/manishrjain/asanawarrior/x"
)

const (
	stamp = "20060102T150405Z"
)

type task struct {
	Completed   string   `json:"end"`
	Created     string   `json:"entry"`
	Description string   `json:"description"`
	Modified    string   `json:"modified"`
	Project     string   `json:"project"`
	Status      string   `json:"status"`
	Tags        []string `json:"tags"`
	Uuid        string   `json:"uuid"`
	Xid         string   `json:"xid"`
}

func (t task) ToWarriorTask() (x.WarriorTask, error) {
	var empty x.WarriorTask
	mts, err := time.Parse(stamp, t.Modified)
	if err != nil {
		return empty, err
	}
	cts, err := time.Parse(stamp, t.Created)
	if err != nil {
		return empty, err
	}
	dts, err := time.Parse(stamp, t.Completed)
	if err != nil {
		return empty, err
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
		Completed: dts,
		Created:   cts,
		Modified:  mts,
		Name:      t.Description,
		Project:   t.Project,
		Section:   sec,
		Tags:      tags,
		Xid:       xid,
	}
	return wt, nil
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
		if wt, err := t.ToWarriorTask(); err == nil {
			wtasks = append(wtasks, wt)
		} else {
			log.Printf("Error while converting task to WarriorTask: %+v", err)
		}
	}
	return wtasks, nil
}

func createNew(wt x.WarriorTask) task {
	status := "pending"
	if !wt.Completed.IsZero() {
		status = "completed"
	}

	tags := make([]string, 0, 10)
	copy(tags, wt.Tags)
	if len(wt.Assignee) > 0 {
		tags = append(tags, "@"+wt.Assignee)
	}
	if len(wt.Section) > 0 {
		tags = append(tags, "#"+wt.Section)
	}

	t := task{
		Completed:   wt.Completed.Format(stamp),
		Created:     wt.Created.Format(stamp),
		Description: wt.Name,
		Modified:    wt.Modified.Format(stamp),
		Project:     wt.Project,
		Status:      status,
		Tags:        tags,
		Xid:         strconv.FormatUint(wt.Xid, 10),
	}
	return t
}

func AddNew(wt x.WarriorTask) error {
	t := createNew(wt)
	body, err := json.Marshal(t)
	if err != nil {
		return err
	}

	cmd := fmt.Sprintf("echo -n %q | task import", body)
	out, err := exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}
