package fetcher

import (
	"crypto/sha1"
	"encoding/hex"
	"github.com/aagat/attic/config"
	"github.com/aagat/attic/models"
	"github.com/aagat/attic/search"
	"github.com/goware/urlx"
	m "github.com/keighl/metabolize"
	"io/ioutil"
	"log"
	"mime"
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

func (f *Fetcher) Worker(id int, jobs <-chan string, results chan<- *models.BookmarkMeta, errors chan<- string) {
	log.Println("Worker Online. Worker no:", id)
	for url := range jobs {
		hash := Hash(url)

		// Get bookmarks object first. We'll use this for indexing.
		b, err := f.models.GetBookmarkByHash(hash)
		if err != nil {
			log.Println(err)
			errors <- hash
			continue
		}

		purl, err := urlx.Parse(url)
		if err != nil {
			log.Println(err)
			errors <- url
			continue
		}

		if purl.Scheme != "http" && purl.Scheme != "https" {
			log.Println("Invalid scheme/protocol")
			errors <- url
			continue
		}

		normalized, err := urlx.Normalize(purl)

		if err != nil {
			log.Println(err)
			errors <- url
			continue
		}

		log.Println("Downloading:", purl)
		resp, err := http.Get(normalized)
		if err != nil {
			log.Println(err)
			errors <- url
			continue
		}

		ty, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
		if err != nil {
			log.Println(err)
			errors <- url
			continue
		}
		defer resp.Body.Close()

		if IsIndexableType(ty) {
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				log.Println(err)
				errors <- hash
				continue
			}

			b.Text = string(body)

			metadata := new(models.BookmarkMeta)

			err = m.Metabolize(resp.Body, metadata)
			if err != nil {
				log.Println(err)
				errors <- hash
				continue
			}

			metadata.Bookmark = hash
			metadata.Url = url
			metadata.KeywordsToArray(metadata.RawKeywords)

			results <- metadata
		} else {
			errors <- url
		}
		go f.search.Index(hash, b)
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

func IsIndexableType(ty string) bool {

	accepted_types := []string{
		"text/html",
		"application/xhtml+xml",
		"application/xml",
		"application/json",
		"text/plain",
	}

	for _, t := range accepted_types {
		if t == ty {
			return true
		}
	}
	return false

}
