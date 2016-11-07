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

func runRequest(method, url string) ([]byte, error) {
RUNLOOP:
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		log.Fatal(err)
	}

	req.Header.Add("Authorization", "Bearer "+*token)
	client := &http.Client{
		Timeout: 10 * time.Minute,
	}

	resp, err := client.Do(req)
	statusCode := resp.StatusCode
	if err != nil || statusCode != http.StatusOK {
		log.Printf("runRequest method: [%v] url: [%v] status: [%v] err: [%v]", method, url, http.StatusText(statusCode), err)
		time.Sleep(5 * time.Second)
		goto RUNLOOP
	}
	defer resp.Body.Close()
	return ioutil.ReadAll(resp.Body)
}

func runGetter(i interface{}, suffix string, fields ...string) error {
	url := fmt.Sprintf("%s/%s?opt_fields=%s", prefix, suffix, strings.Join(fields, ","))
	body, err := runRequest("GET", url)
	if err != nil {
		return errors.Wrapf(err, "runGetter: %q", body)
	}
	if err := json.Unmarshal(body, i); err != nil {
		return errors.Wrapf(err, "Unmarshal: %q", body)
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

func getTasks(proj Basic, out chan x.WarriorTask, errc chan error) {
	var sectionName string
	var t tasks
	if err := runGetter(&t, fmt.Sprintf("projects/%d/tasks", proj.Id),
		"assignee,name,tags,completed_at,modified_at,created_at"); err != nil {
		errc <- errors.Wrapf(err, "getTasks for project: %v", proj.Name)
		return
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
			errc <- errors.Wrapf(err, "convert: getTasks for project: %v", proj.Name)
			return
		}
		out <- wt
	}
	errc <- nil
}

func GetTasks() ([]x.WarriorTask, error) {
	if err := cache.update(); err != nil {
		return nil, errors.Wrap(err, "cache.update")
	}

	out := make(chan x.WarriorTask, 100)
	projects := cache.Projects()
	errc := make(chan error, len(projects))
	for _, proj := range projects {
		go getTasks(proj, out, errc)
	}

	// Asana can send back the same task multiple times, if it's part of multiple projects.
	// So, let's dedup them.
	seen := make(map[uint64]bool)
	wtasks := make([]x.WarriorTask, 0, 100)
	done := make(chan struct{})
	go func() {
		for wt := range out {
			if _, has := seen[wt.Xid]; has {
				// ignore
				continue
			}
			wtasks = append(wtasks, wt)
			seen[wt.Xid] = true
		}
		done <- struct{}{}
	}()

	var rerr error
	for _ = range projects {
		if err := <-errc; err != nil {
			rerr = err
		}
	}
	close(out)
	<-done // Wait for all tasks to be picked up by goroutine.
	return wtasks, rerr
}

// runPost would run a PUT or POST to Asana. No locks should be acquired.
func runPost(method, suffix string, values url.Values) ([]byte, error) {
POSTLOOP:
	url := fmt.Sprintf("%s/%s", prefix, suffix)
	fmt.Println(url, values.Encode())
	req, err := http.NewRequest(method, url, bytes.NewBufferString(values.Encode()))
	if err != nil {
		log.Fatal(errors.Wrap(err, "runPost http.NewRequest"))
	}

	req.Header.Add("Authorization", "Bearer "+*token)
	req.Header.Add("content-type", "application/x-www-form-urlencoded")
	client := &http.Client{
		Timeout: 10 * time.Minute,
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("runPost url: [%v] err: [%v]", url, err)
		time.Sleep(5 * time.Second)
		goto POSTLOOP
	}
	defer resp.Body.Close()

	return ioutil.ReadAll(resp.Body)
}

func toTagIds(tnames []string) []string {
	var tags []string
	for _, t := range tnames {
		tid := cache.TagId(t)
		if tid == 0 {
			tid = cache.CreateTag(t)
			fmt.Printf("New Tag created. ID: %d", tid)
		}
		if tid > 0 {
			tags = append(tags, strconv.FormatUint(tid, 10))
		}
	}
	return tags
}

func removeProject(tid, pid uint64) error {
	v := url.Values{}
	v.Add("project", strconv.FormatUint(pid, 10))
	_, err := runPost("POST", fmt.Sprintf("tasks/%d/removeProject", tid), v)
	return err
}

func updateSection(tid, pid uint64, section string) error {
	v := url.Values{}
	v.Add("project", strconv.FormatUint(pid, 10))

	sid := cache.SectionId(pid, section)
	if sid > 0 {
		v.Add("section", strconv.FormatUint(sid, 10))
	}

	_, err := runPost("POST", fmt.Sprintf("tasks/%d/addProject", tid), v)
	return err
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
	aid := cache.UserId(wt.Assignee)
	if aid > 0 {
		v.Add("assignee", strconv.FormatUint(aid, 10))
	}
	if !wt.Completed.IsZero() {
		v.Add("completed", "true")
	}

	tags := toTagIds(wt.Tags)
	v.Add("tags", strings.Join(tags, ","))
	resp, err := runPost("POST", "tasks", v)
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
	if err := updateSection(ot.Data.Id, pid, wt.Section); err != nil {
		return e, errors.Wrap(err, "AddNew updateSection")
	}

	// Now retrieve the task back again so we can sync it up with TW.
	return GetOneTask(ot.Data.Id)
}

func diff(t1 []string, t2 []string) []string {
	m := make(map[string]bool)
	for _, s := range t2 {
		m[s] = true
	}

	var result []string
	for _, t := range t1 {
		if has := m[t]; !has {
			result = append(result, t)
		}
	}
	return result
}

func updateOneTag(tagid, taskid, instruction string, errc chan error) {
	v := url.Values{}
	v.Add("tag", tagid)
	suffix := fmt.Sprintf("tasks/%s/%s", taskid, instruction)
	_, err := runPost("POST", suffix, v)
	if err != nil {
		errc <- errors.Wrap(err, "updateTags")
		return
	}
	errc <- nil
}

func updateTags(tw x.WarriorTask, asana x.WarriorTask) error {
	taskid := strconv.FormatUint(tw.Xid, 10)
	add := diff(tw.Tags, asana.Tags)
	rem := diff(asana.Tags, tw.Tags)

	addids := toTagIds(add)
	remids := toTagIds(rem)
	sz := len(addids) + len(remids)

	errc := make(chan error, sz)
	for _, id := range addids {
		go updateOneTag(id, taskid, "addTag", errc)
	}
	for _, id := range remids {
		go updateOneTag(id, taskid, "removeTag", errc)
	}

	var rerr error
	for i := 0; i < sz; i++ {
		if err := <-errc; err != nil {
			rerr = err
		}
	}

	return rerr
}

func UpdateTask(tw x.WarriorTask, asana x.WarriorTask) error {
	v := url.Values{}
	if tw.Name != asana.Name {
		v.Add("name", tw.Name)
	}
	if tw.Assignee != asana.Assignee {
		a := cache.UserId(tw.Assignee)
		if a > 0 {
			v.Add("assignee", strconv.FormatUint(a, 10))
		}
	}
	if !tw.Completed.IsZero() && asana.Completed.IsZero() {
		v.Add("completed", "true")
	}

	if len(v) > 0 {
		resp, err := runPost("PUT", "tasks/"+strconv.FormatUint(tw.Xid, 10), v)
		if err != nil {
			return errors.Wrap(err, "UpdateAsanaTask")
		}
		fmt.Println(string(resp))
	}

	if err := updateTags(tw, asana); err != nil {
		return errors.Wrap(err, "asana.UpdateTask updateTags")
	}

	// Update project or section if changed.
	pid := cache.ProjectId(tw.Project)
	if pid > 0 && (tw.Project != asana.Project || tw.Section != asana.Section) {
		fmt.Printf("Updating project and section: %v %v\n", tw.Project, tw.Section)
		if err := updateSection(tw.Xid, pid, tw.Section); err != nil {
			return errors.Wrap(err, "asana.UpdateTask updateSection")
		}
		if tw.Project == asana.Project {
			return nil
		}
		// Project was changed. So, remove the last one.
		fmt.Printf("Removing from project: %v\n", asana.Project)
		if previd := cache.ProjectId(asana.Project); previd > 0 {
			if err := removeProject(tw.Xid, previd); err != nil {
				return err
			}
		}
	}
	return nil
}

func GetOneTask(taskid uint64) (x.WarriorTask, error) {
	e := x.WarriorTask{}
	var ot oneTask
	if err := runGetter(&ot, "tasks/"+strconv.FormatUint(taskid, 10)); err != nil {
		return e, errors.Wrap(err, "AddNew runGetter")
	}

	if len(ot.Data.Memberships) == 0 {
		return e, fmt.Errorf("Member of no project")
	}
	member := ot.Data.Memberships[0]

	sname := cache.SectionName(member.Project.Id, member.Section.Id)
	return convert(ot.Data, member.Project.Name, sname)
}

func Delete(taskid uint64) error {
	url := fmt.Sprintf("%s/tasks/%d", prefix, taskid)
	_, err := runRequest("DELETE", url)
	return err
}
