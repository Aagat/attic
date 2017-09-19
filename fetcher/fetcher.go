package fetcher

import (
	"crypto/sha1"
	"encoding/hex"
	"github.com/aagat/attic/config"
	"github.com/aagat/attic/models"
	"github.com/aagat/attic/search"
	m "github.com/keighl/metabolize"
	"io/ioutil"
	"log"
	"net/http"
)

type Fetcher struct {
	jobs    chan string
	results chan *models.BookmarkMeta
	errors  chan string
	search  *search.Search
	models  *models.Models
}

func Init(c *config.Config, jobs chan string, results chan *models.BookmarkMeta, errors chan string) *Fetcher {
	return &Fetcher{
		models:  c.Models.(*models.Models),
		search:  c.Search.(*search.Search),
		jobs:    jobs,
		results: results,
		errors:  errors,
	}
}

func (f *Fetcher) Boot(num int) {
	for w := 1; w <= num; w++ {
		go f.Worker(w, f.jobs, f.results, f.errors)
	}
}

func (f *Fetcher) Worker(id int, jobs <-chan string, result chan<- *models.BookmarkMeta, errors chan<- string) {
	log.Println("Worker Online. Worker no:", id)
	for url := range jobs {
		hash := Hash(url)
		// Get bookmarks object first. We'll use this for indexing.
		b, err := f.models.GetBookmarkByHash(hash)
		if err != nil {
			log.Fatal(err)
		}

		// TODO
		// Sanitize url and make sure there is protocol specified
		log.Println(url)
		resp, err := http.Get(url)
		if err != nil {
			errors <- hash
			log.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			errors <- hash
			log.Fatal(err)
		}

		b.Text = string(body)

		log.Printf("%+v", body)
		log.Println(string(body))

		metadata := new(models.BookmarkMeta)

		err = m.Metabolize(resp.Body, metadata)
		if err != nil {
			errors <- hash
			log.Fatal(err)
		}

		metadata.Bookmark = hash
		metadata.KeywordsToArray(metadata.RawKeywords)

		go f.search.Index(hash, b)

		result <- metadata
	}
}

func (f *Fetcher) Fetch(url string) {
	f.jobs <- url
}

func Hash(url string) string {
	hash := sha1.New()
	hash.Write([]byte(url))
	return hex.EncodeToString(hash.Sum(nil))
}
