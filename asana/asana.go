package asana

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/manishrjain/asanawarrior/x"
	"github.com/pkg/errors"
)

var token = flag.String("token", "", "Token provided by Asana.")
var cache *acache = new(acache)

const (
	prefix = "https://app.asana.com/api/1.0"
	stamp  = "2006-01-02T15:04:05.999Z"
)

func runGetter(i interface{}, suffix string, fields ...string) error {
	url := fmt.Sprintf("%s/%s?opt_fields=%s", prefix, suffix, strings.Join(fields, ","))
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Add("Authorization", "Bearer "+*token)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, i); err != nil {
		return err
	}
	return nil
}

type Basic struct {
	Id    uint64 `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}
type BasicData struct {
	Data []Basic `json:"data"`
}

func getVarious(suffix string, opts ...string) ([]Basic, error) {
	var bd BasicData
	if err := runGetter(&bd, suffix, opts...); err != nil {
		return nil, err
	}
	return bd.Data, nil
}

type task struct {
	Basic
	Assignee    Basic   `json:"assignee"`
	Tags        []Basic `json:"tags"`
	CompletedAt string  `json:"completed_at"`
	ModifiedAt  string  `json:"modified_at"`
	CreatedAt   string  `json:"created_at"`
}

type tasks struct {
	Data []task `json:"data"`
}

func convert(tsk task, proj, section string) (x.WarriorTask, error) {
	e := x.WarriorTask{}

	mts, err := time.Parse(stamp, tsk.ModifiedAt)
	if err != nil {
		return e, errors.Wrap(err, "asana modified at")
	}
	cts, err := time.Parse(stamp, tsk.CreatedAt)
	if err != nil {
		return e, errors.Wrap(err, "asana created at")
	}
	var dts time.Time
	if len(tsk.CompletedAt) > 0 {
		dts, err = time.Parse(stamp, tsk.CompletedAt)
		if err != nil {
			return e, errors.Wrap(err, "asana completed at")
		}
	}

	wt := x.WarriorTask{
		Name:      tsk.Name,
		Project:   proj,
		Xid:       tsk.Id,
		Assignee:  cache.User(tsk.Assignee.Id),
		Modified:  mts,
		Created:   cts,
		Completed: dts,
		Section:   section,
	}
	for _, tag := range tsk.Tags {
		wt.Tags = append(wt.Tags, cache.Tag(tag.Id))
	}
	return wt, nil
}

func GetTasks(max int) ([]x.WarriorTask, error) {
	if err := cache.update(); err != nil {
		return nil, errors.Wrap(err, "cache.update")
	}

	wtasks := make([]x.WarriorTask, 0, 100)
	var section string
	count := 0
LOOP:
	for _, proj := range cache.Projects() {
		var t tasks
		if err := runGetter(&t, fmt.Sprintf("projects/%d/tasks", proj.Id),
			"assignee,name,tags,completed_at,modified_at,created_at"); err != nil {
			return nil, err
		}
		for _, tsk := range t.Data {
			if len(tsk.Name) == 0 {
				// Don't sync such tasks.
				continue
			}
			if strings.HasSuffix(tsk.Name, ":") {
				section = strings.Map(func(r rune) rune {
					if 'A' <= r && r <= 'Z' || 'a' <= r && r <= 'z' || '0' <= r && r <= '9' {
						return r
					}
					return -1
				}, tsk.Name)

				continue
			}
			wt, err := convert(tsk, proj.Name, section)
			if err != nil {
				return nil, errors.Wrap(err, "GetTasks")
			}
			wtasks = append(wtasks, wt)
			count++
			if count >= max {
				break LOOP
			}
		}
	}
	return wtasks, nil
}

func AddNew(wt x.WarriorTask) error {
	return nil
}
