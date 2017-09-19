package fetcher

import (
	"database/sql"
	"github.com/aagat/attic/config"
	"github.com/aagat/attic/models"
	"github.com/aagat/attic/search"
	m "github.com/keighl/metabolize"
	"log"
	"net/http"
)

type Fetcher struct {
	jobs    chan string
	results chan *models.BookmarkMeta
	errors  chan string
	DB      *sql.DB
	index   *search.Search
}

type PageInfo struct {
	Title       string `meta:"og:title,title"`
	Description string `meta:"og:description,description"`
	Keywords    string `meta:"keywords"`
	Type        string `meta:"og:type"`
}

func Init(c *config.Config, jobs chan string, results chan *models.BookmarkMeta, errors chan string) *Fetcher {
	return &Fetcher{
		DB:      c.DB.(*sql.DB),
		index:   c.Search.(*search.Search),
		jobs:    jobs,
		results: results,
		errors:  errors,
	}
}

func (f *Fetcher) Boot(num int) {
	for w := 1; w <= num; w++ {
		go Worker(w, f.jobs, f.results, f.errors)
	}
}

func Worker(id int, jobs <-chan string, result chan<- *models.BookmarkMeta, errors chan<- string) {
	log.Println("Worker Online. Worker no:", id)
	for url := range jobs {
		log.Println(url)
		resp, err := http.Get(url)
		if err != nil {
			panic(err)
		}
		defer resp.Body.Close()

		metadata := new(models.BookmarkMeta)

		err = m.Metabolize(resp.Body, metadata)
		if err != nil {
			log.Fatal(err)
		}

		result <- metadata

		log.Printf("%+v", metadata)
	}
}

func (f *Fetcher) Fetch(url string) {
	f.jobs <- url
}
