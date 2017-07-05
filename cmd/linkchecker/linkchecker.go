package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sync"
	"time"
)

type CheckResult struct {
	HTTPCode int
	Referrer string
	Error    error
	Body     string
	Recursed bool
}

var linksChecked map[string]*CheckResult
var host string

func main() {
	flag.StringVar(&host, "host", "", "Hostname and port of site to check.")
	flag.Parse()

	// Just add http:// to the host name and go.
	link := "http://" + host
	log.Println("Checking:", link)

	// Map that will hold all the link results.
	linksChecked = make(map[string]*CheckResult)

	// Download the root page.
	start := time.Now()
	cr := download("", link)
	if cr.Error != nil {
		log.Fatal(cr.Error)
	}
	linksChecked[link] = cr

	// Recurse through the rest of the site.
	recurse(link, cr.Body)

	// Summarize results.
	log.Println("--------------------------------------------------------------")
	var fives, fours, threes, twos, errors int
	for link, cr := range linksChecked {
		switch {
		case cr.HTTPCode >= 500:
			fives++
		case cr.HTTPCode >= 400:
			fours++
		case cr.HTTPCode >= 300:
			threes++
		case cr.HTTPCode >= 200:
			twos++
		default:
			errors++
		}

		if cr.HTTPCode < 200 || cr.HTTPCode > 299 {
			// Log the errors again at the bottom for convience.
			log.Printf("Referrer: %s Link: %s HTTPCode: %d\n", cr.Referrer, link, cr.HTTPCode)
		}
	}

	dur := time.Since(start)
	log.Println("--------------------------------------------------------------")
	log.Printf("Duration: %.0fs", dur.Seconds())
	log.Printf("Results 500s: %d 400s: %d 300s: %d 200s: %d Errors: %d",
		fives, fours, threes, twos, errors)

	if fives+fours+threes+errors > 0 {
		os.Exit(1)
	}
}

// recurse parses the html passed for urls, it takes the referrer link
// to build relative links.
func recurse(link, html string) {
	// Parse all the links from the html
	ls := parseLinks(link, html)

	// Loop through all the links and download asynchronously.
	var wg sync.WaitGroup
	var mutex = &sync.Mutex{}
	for _, l := range ls {

		// If link not already checked, download.
		if _, ok := linksChecked[l]; !ok {

			// Download in a new routine.
			wg.Add(1)
			go func(referrer, link string) {
				defer wg.Done()
				cr := download(referrer, link)

				// Write result to links checked map.
				mutex.Lock()
				linksChecked[link] = cr
				mutex.Unlock()

				log.Printf("Referrer: %s Link: %s HTTPCode: %d\n", cr.Referrer, link, cr.HTTPCode)
			}(link, l)
		}
	}
	wg.Wait()

	linksChecked[link].Recursed = true

	// Loop through the downloaded links and recurse
	for _, l := range ls {
		// If image don't recurse, continue to next link.
		if isImage(l) {
			continue
		}

		// If the link has not been recursed yet and for current host
		// then recurse through it.
		if !linksChecked[l].Recursed {
			r := fmt.Sprintf("http(s)?://(www\\.)?" + host + ".*")
			if found, _ := regexp.Match(r, []byte(l)); found {
				recurse(l, linksChecked[l].Body)
			}
		}
	}
}

// isImage returns true if a url is for an image.
func isImage(url string) bool {
	if found, _ := regexp.Match("(jpg|svg|gif|png)$", []byte(url)); found {
		return true
	}
	return false
}

// download gets the url passed returns an error or the html
// and the status code.
func download(referrer, url string) *CheckResult {
	cr := &CheckResult{Referrer: referrer}

	client := http.Client{Timeout: time.Duration(15 * time.Second)}
	response, err := client.Get(url)
	if err != nil {
		cr.Error = err
		return cr
	}
	cr.HTTPCode = response.StatusCode

	// If image don't download body.
	if isImage(url) {
		return cr
	}

	// Download html body.
	defer response.Body.Close()
	b, err := ioutil.ReadAll(response.Body)
	if err != nil {
		cr.Error = err
		return cr
	}
	cr.Body = string(b)

	return cr
}

// parseLinks parses html s for urls and returns them as a slice.
func parseLinks(link, s string) []string {
	u, err := url.Parse(link)
	if err != nil {
		log.Println(err, ":", link)
	}
	var links []string

	// Get anything that looks like an absolute url.
	r := regexp.MustCompile("('|\")http(s)?://[^\"']*\"")
	for _, l := range r.FindAllString(s, -1) {
		nl := l[1 : len(l)-1]
		links = append(links, nl)
	}

	// Get anything that looks like a relative url.
	// Add the hostname.
	r = regexp.MustCompile("\"/[^\"]*\"")
	for _, l := range r.FindAllString(s, -1) {
		nl := l[1 : len(l)-1]
		links = append(links, u.Scheme+"://"+u.Host+nl)
	}

	return links
}