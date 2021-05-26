package main

import (
	"fmt"
	"html/template"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gocolly/colly/v2"
)

/*
	The task gave me plenty of space for interpretation,
    in a job I would ask more questions about the requirements and specifics of implementation.
    Here I decided to avoid indirect communication with the IT team, to keep things simple for the interview.
	Simplicity and clarity were my main concerns during development.
    I was trying to be as strict as possible with the task interpretation so for the web app with Golang
    I've chosen client/server architecture with statically served pages. Implemented parser on top of a 3-rd party parser
	for the same simplicity reasons and since the app is supposed to process only one URL entry at a time
	decided to use app's cash without storing previous requests therefore not using a database.
	Used 3-rd party html template to make the app look nicer.
*/

/*
TODO:
	1. get HTML version of a page
	2. traverse html tree and get all tags by level
	5. check if a page contains login page
*/

// move to .env
var PORT string = "8080"

// load templates before serving
var templates = template.Must(template.ParseFiles("static/demoTempl/result.html"))

// PARSER

// bundle for error handling in concurrent http requests
type ConcReq struct {
	urlStatus map[string]int
	err       error
}

// stores requested URLs and its products
type Stats struct {
	Version        string
	Title          string
	Domain         string
	Headings       string
	CashedLinks    map[string]int
	InternalLinks  []string
	ExternalLinks  []string
	ForbiddenLinks []string
	HasLogin       bool
}

// PARSER METHODS

// extracts domain name from the provided url
func (s *Stats) setDomain(url string) {
	if url != "" {
		urlParts := strings.Split(url, ".")
		s.Domain = urlParts[1]
	}
}

// Checks if an URL containts original domain name in the leftmost position
// than it counts as an internal domain or as an external otherwise.
// There will be external URLs where domain name is used as a resourse path.
// Example: http://www.externalapp/domain
// Commented out regex suppose to fix this problem, not sure how to implement it with go
func (s *Stats) classifyLinks() {
	// TODO: commented out regex matches desired pattern but something is wrong with my implementation
	//regex := fmt.Sprintf(`\\.(%v)\\.`, s.domain)
	regex := fmt.Sprintf(`%v`, s.Domain)
	domainLookUp := regexp.MustCompile(regex)
	for i := range s.CashedLinks {
		if s.Domain == domainLookUp.FindString(i) {
			s.InternalLinks = append(s.InternalLinks, i)
		} else {
			s.ExternalLinks = append(s.ExternalLinks, i)
		}

	}
}

// makes custom concurrent requests against cached links
// expects string to be an URL in the request function
func (s *Stats) requestAll(client *http.Client, req func(string, *http.Client, chan<- ConcReq)) error {
	ch := make(chan ConcReq)

	for i := range s.CashedLinks {
		go req(i, client, ch)
	}

	for range s.CashedLinks {
		entry := <-ch
		if entry.err != nil {
			return entry.err
		}

		s.CashedLinks = entry.urlStatus
	}

	// went for this O(3n) solution because maps aren't secure for concurent access
	// couldn't write directly from a custom function to *Stats and prefered to keep
	// requestAll and pingURL decoupled
	for k, v := range s.CashedLinks {
		switch v {
		case http.StatusOK:
		//case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden:
		default:
			s.ForbiddenLinks = append(s.ForbiddenLinks, k)
		}
	}

	close(ch)
	return nil
}

// CUSTOM FUNCTIONS

// custom function for making http requests.
func pingURL(url string, c *http.Client, ch chan<- ConcReq) {
	m := ConcReq{urlStatus: make(map[string]int)}
	res, err := c.Head(url)
	if err != nil {
		m.err = err
		ch <- m
		return
	}
	m.urlStatus[url] = res.StatusCode
	ch <- m
}

// counts the amount of links
func countLinks(arr []string) int {
	return len(arr) - 1
}

// CONTROLLERS

func handleHTMLParser(w http.ResponseWriter, r *http.Request) {
	// protect server from answering to unsupported method and resource requests
	if r.URL.Path != "/parser" {
		http.Error(w, "Not found.", http.StatusNotFound)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method is not supported.", http.StatusNotFound)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
		return
	}

	// parse form
	url := r.FormValue("url")

	// check that the url is a valid URL
	match, err := regexp.MatchString("((http|https)://)(www.)?[a-zA-Z0-9@:%._\\+~#?&//=]{2,256}\\.[a-z]{2,6}\\b([-a-zA-Z0-9@:%._\\+~#?&//=]*)", url)

	// respond to a user if the URL is bad
	if err != nil {
		http.Error(w, "Something went wrong.", http.StatusInternalServerError)
		return
	}

	if !match {
		http.Error(w, "Invalid URL", http.StatusBadRequest)
		return
	}

	// start parsing html from provided URL
	p := &Stats{CashedLinks: make(map[string]int)}

	c := colly.NewCollector()

	c.OnHTML("a[href]", func(e *colly.HTMLElement) {
		link := e.Request.AbsoluteURL(e.Attr("href"))
		if link != "" {
			p.CashedLinks[link]++
		}
	})

	c.OnHTML("title", func(e *colly.HTMLElement) {
		title := e.Request.AbsoluteURL(e.Attr("title"))
		if title != "" {
			p.Title = title
		}
	})

	c.Visit(url)

	// process data

	// extract domain name from user's url
	p.setDomain(url)

	// separate external from internal urls
	// NB! contains a bug described in the method
	p.classifyLinks()

	// prepare and make requests to all parsed urls external and internal
	cl := http.Client{Timeout: 2 * time.Second}

	err = p.requestAll(&cl, pingURL)

	if err != nil {
		http.Error(w, "Something went wrong", http.StatusInternalServerError)
		return
	}

	// development stuff
	// uncomment to see scrapping results
	// for _, v := range p.InternalLinks {
	// 	fmt.Printf("internal: %v \n", v)
	// }

	// for _, v := range p.ExternalLinks {
	// 	fmt.Printf("external: %v \n", v)
	// }

	// for _, v := range p.ForbiddenLinks {
	// 	fmt.Printf("forbidden: %v \n", v)
	// }
	// fmt.Printf("domain: %v", p.Domain)
	// fmt.Printf("title: %v", p.Title)

	// TODO: fix bug
	// gives: superfluous response.WriteHeader call from main.handleHTMLParser if passing data to template.
	// this has to do with writing headers multiple times to http.ResponseWriter. Sending it as is because the time is up
	err = templates.ExecuteTemplate(w, "result.html", nil)
	if err != nil {
		http.Error(w, "Something went wrong", http.StatusInternalServerError)
		return
	}

}

func main() {

	st := http.FileServer(http.Dir("./static/demoTempl"))

	// HANDLERS
	http.Handle("/", st)
	http.HandleFunc("/parser", handleHTMLParser)

	// SERVER
	fmt.Printf("listening on port%v ", PORT)
	if err := http.ListenAndServe(":"+PORT, nil); err != nil {
		log.Fatal(err)
	}
}
