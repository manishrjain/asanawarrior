package asana

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/manishrjain/asanawarrior/x"
	"github.com/pkg/errors"
)

var token = flag.String("token", "", "Token provided by Asana.")
var domain = flag.String("domain", "", "Workspace name, generally your domain name in Asana.")
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
type BasicDataOne struct {
	Data Basic `json:"data"`
}

func getVarious(suffix string, opts ...string) ([]Basic, error) {
	var bd BasicData
	if err := runGetter(&bd, suffix, opts...); err != nil {
		return nil, err
	}
	return bd.Data, nil
}

type psec struct {
	Project Basic `json:"project"`
	Section Basic `json:"section"`
}

type task struct {
	Basic
	Assignee    Basic   `json:"assignee"`
	Tags        []Basic `json:"tags"`
	CompletedAt string  `json:"completed_at"`
	ModifiedAt  string  `json:"modified_at"`
	CreatedAt   string  `json:"created_at"`
	Memberships []psec  `json:"memberships"`
}

type tasks struct {
	Data []task `json:"data"`
}

type oneTask struct {
	Data task `json:"data"`
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
	var sectionName string
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
				sec := Basic{
					Id:   tsk.Id,
					Name: tsk.Name,
				}
				sectionName = cache.AddSection(proj.Id, sec)
				continue
			}

			wt, err := convert(tsk, proj.Name, sectionName)
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

// runPost would run a POST to Asana. No locks should be acquired.
func runPost(suffix string, values url.Values) ([]byte, error) {
	url := fmt.Sprintf("%s/%s", prefix, suffix)
	fmt.Println(url, values.Encode())
	req, err := http.NewRequest("POST", url, bytes.NewBufferString(values.Encode()))
	if err != nil {
		log.Fatal(errors.Wrap(err, "runPost http.NewRequest"))
	}

	req.Header.Add("Authorization", "Bearer "+*token)
	req.Header.Add("content-type", "application/x-www-form-urlencoded")
	client := &http.Client{}
	resp, err := client.Do(req)
	defer resp.Body.Close()

	return ioutil.ReadAll(resp.Body)
}

func AddNew(wt x.WarriorTask) (x.WarriorTask, error) {
	e := x.WarriorTask{}

	// Ensure that project actually exists before proceeding.
	pid := cache.ProjectId(wt.Project)
	if pid == 0 {
		return e, fmt.Errorf("Project not found: %v", wt.Project)
	}

	v := url.Values{}
	v.Add("workspace", strconv.FormatUint(cache.Workspace(), 10))
	v.Add("name", wt.Name)
	fmt.Println("Assignee", wt.Assignee)
	aid := cache.UserId(wt.Assignee)
	if aid > 0 {
		v.Add("assignee", strconv.FormatUint(aid, 10))
	}

	var tags []string
	for _, t := range wt.Tags {
		tid := cache.TagId(t)
		if tid == 0 {
			tid = cache.CreateTag(t)
			fmt.Println("New Tag crated. ID: %d", tid)
		}
		if tid > 0 {
			tags = append(tags, strconv.FormatUint(tid, 10))
		}
	}
	v.Add("tags", strings.Join(tags, ","))
	resp, err := runPost("tasks", v)
	if err != nil {
		return e, errors.Wrap(err, "AddNew runPost")
	}
	fmt.Println(string(resp))

	var ot oneTask
	if err := json.Unmarshal(resp, &ot); err != nil {
		return e, errors.Wrap(err, "AddNew unmarshal")
	}
	if ot.Data.Id == 0 {
		return e, fmt.Errorf("Unable to find ID assigned by Asana: %+v", ot.Data)
	}

	// Now set the project and section.
	sid := cache.SectionId(pid, wt.Section)
	v = url.Values{}
	v.Add("project", strconv.FormatUint(pid, 10))
	if sid > 0 {
		v.Add("section", strconv.FormatUint(sid, 10))
	}
	_, err = runPost(fmt.Sprintf("tasks/%d/addProject", ot.Data.Id), v)
	if err != nil {
		return e, errors.Wrap(err, "addProject runPost")
	}

	// Now retrieve the task back again so we can sync it up with TW.
	if err := runGetter(&ot, "tasks/"+strconv.FormatUint(ot.Data.Id, 10)); err != nil {
		return e, errors.Wrap(err, "AddNew runGetter")
	}
	if len(ot.Data.Memberships) == 0 {
		return e, fmt.Errorf("Member of no project")
	}
	member := ot.Data.Memberships[0]

	sname := cache.SectionName(member.Project.Id, member.Section.Id)
	return convert(ot.Data, member.Project.Name, sname)
}
