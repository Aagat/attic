package search

import (
	"github.com/blevesearch/bleve"
	bleveHttp "github.com/blevesearch/bleve/http"
	"log"
)

type Search struct {
	index bleve.Index
}

func Init(path string) (*Search, error) {

	index, err := bleve.Open(path)

	if err != nil && err != bleve.ErrorIndexPathDoesNotExist {
		return nil, err
	}

	if err == bleve.ErrorIndexPathDoesNotExist {
		log.Println("No index found, creating index.")
		mapping := buildMapping()
		index, err = bleve.New(path, mapping)

		if err != nil {
			return nil, err
		}
	}
	return &Search{index: index}, nil
}

func (s *Search) Index(key string, val interface{}) {
	s.index.Index(key, val)
}

func (s *Search) Delete(key string) error {
	return s.index.Delete(key)
}

func (s *Search) SearchHandler() *bleveHttp.SearchHandler {
	bleveHttp.RegisterIndexName("bookmarks", s.index)
	searchHandler := bleveHttp.NewSearchHandler("bookmarks")
	return searchHandler
}
