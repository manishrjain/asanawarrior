package asana

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
)

var token = flag.String("token", "", "Token provided by Asana.")

const (
	prefix = "https://app.asana.com/api/1.0"
)

type User struct {
	Id    uint64 `json:"id"`
	Email string `json:"email"`
}

func runGetter(suffix string, fields ...string) {
	url := fmt.Sprintf("%s/suffix?opt_fields=%s", prefix, suffix, strings.Join(fields, ","))
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Add("Authorization", "Bearer "+*token)
	fmt.Println(http.Get(url))
}

func Users() {
	runGetter("users", "email")
}
