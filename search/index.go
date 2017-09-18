package search

import (
	"github.com/blevesearch/bleve"
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

func (s *Search) Add(key string, val interface{}) {
	s.index.Index(key, val)
}
