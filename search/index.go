package search

import (
	"github.com/blevesearch/bleve"
	"log"
)

type Index struct {
	index bleve.Index
}

func Init(path string) (*Index, error) {

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

	return &Index{index: index}, nil
}

func (i *Index) Add(key string, val interface{}) {
	i.index.Index(key, val)
}
