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
	"github.com/pkg/errors"
)

const (
	stamp = "20060102T150405Z"
)

type task struct {
	Completed   string   `json:"end,omitempty"`
	Created     string   `json:"entry,omitempty"`
	Description string   `json:"description,omitempty"`
	Modified    string   `json:"modified,omitempty"`
	Project     string   `json:"project,omitempty"`
	Status      string   `json:"status,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Uuid        string   `json:"uuid,omitempty"`
	Xid         string   `json:"xid,omitempty"`
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
	var dts time.Time
	if len(t.Completed) > 0 {
		dts, err = time.Parse(stamp, t.Completed)
		if err != nil {
			return empty, err
		}
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
		case '_':
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
		Assignee: ass,
		Created:  cts,
		Modified: mts,
		Name:     t.Description,
		Project:  t.Project,
		Section:  sec,
		Tags:     tags,
		Xid:      xid,
		Uuid:     t.Uuid,
	}
	if !dts.IsZero() {
		wt.Completed = dts
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
		if t.Status == "deleted" {
			continue
		}

		if wt, err := t.ToWarriorTask(); err == nil {
			wtasks = append(wtasks, wt)
		} else {
			log.Printf("Error while converting task to WarriorTask: %+v", err)
		}
	}
	return wtasks, nil
}

func generateTags(wt x.WarriorTask) []string {
	tags := make([]string, len(wt.Tags), len(wt.Tags)+2)
	copy(tags, wt.Tags)

	if len(wt.Assignee) > 0 {
		tags = append(tags, "@"+wt.Assignee)
	}
	if len(wt.Section) > 0 {
		tags = append(tags, "_"+wt.Section)
	}
	return tags
}

func createNew(wt x.WarriorTask) task {
	status := "pending"
	if !wt.Completed.IsZero() {
		status = "completed"
	}
	tags := generateTags(wt)

	t := task{
		Created:     wt.Created.Format(stamp),
		Description: wt.Name,
		Modified:    wt.Modified.Format(stamp),
		Project:     wt.Project,
		Status:      status,
		Tags:        tags,
		Xid:         strconv.FormatUint(wt.Xid, 10),
	}
	if !wt.Completed.IsZero() {
		t.Completed = wt.Completed.Format(stamp)
	}
	return t
}

func doImport(t task) error {
	body, err := json.Marshal(t)
	if err != nil {
		return err
	}

	cmd := fmt.Sprintf("echo -n %q | task import", body)
	// fmt.Println(cmd)
	out, err := exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		return errors.Wrapf(err, "doImport [%v] out:%q", cmd, out)
	}
	fmt.Println(string(out))
	return nil
}

func AddNew(wt x.WarriorTask) error {
	t := createNew(wt)
	return doImport(t)
}

func OverwriteUuid(asana x.WarriorTask, uuid string) error {
	t := createNew(asana)
	t.Uuid = uuid
	return doImport(t)
}

func Delete(prev x.WarriorTask) error {
	prev.Completed = time.Now()
	t := createNew(prev)
	t.Uuid = prev.Uuid
	t.Status = "deleted"
	return doImport(t)
}
