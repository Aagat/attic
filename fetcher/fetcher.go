package fetcher

import (
	"database/sql"
	"github.com/aagat/attic/config"
	"github.com/aagat/attic/search"
	m "github.com/keighl/metabolize"
	"log"
	"net/http"
)

type App struct {
	jobs    <-chan string
	results chan<- string
	errors  chan<- string
	DB      *sql.DB
	index   *search.Index
}

type PageInfo struct {
	Title       string `meta:"og:title,title"`
	Description string `meta:"og:description,description"`
	Keywords    string `meta:"keywords"`
	Type        string `meta:"og:type"`
}

func Init(c *config.Config, jobs <-chan string, results chan<- string, errors chan<- string) *App {
	return &App{
		DB:      c.DB.(*sql.DB),
		index:   c.Search.(*search.Index),
		jobs:    jobs,
		results: results,
		errors:  errors,
	}
}

func Boot(num int, jobs <-chan string, results chan<- string, errors chan<- string) {
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
