package fetcher

import (
	"database/sql"
	"github.com/aagat/attic/search"
	m "github.com/keighl/metabolize"
	"log"
	"net/http"
)

type FetcherConfig struct {
	jobs    <-chan string
	results chan<- string
	DB      *sql.DB
	index   *search.Index
}

type PageInfo struct {
	Title       string `meta:"og:title,title"`
	Description string `meta:"og:description,description"`
	Keywords    string `meta:"keywords"`
	Type        string `meta:"og:type"`
}

func Boot(num int, jobs <-chan string, results chan<- string) {
	for w := 1; w <= num; w++ {
		go Worker(jobs, results)
	}
}

func Worker(jobs <-chan string, result chan<- string) {
	for url := range jobs {
		log.Println(url)
		resp, err := http.Get(url)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()

		metadata := new(PageInfo)

		err = m.Metabolize(resp.Body, metadata)
		if err != nil {
			log.Fatal(err)
		}

		result <- url

		log.Printf("%+v", metadata)
	}
}
