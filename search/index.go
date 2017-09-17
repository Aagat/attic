package search

import (
	"github.com/blevesearch/bleve"
)

type Index struct {
	index bleve.Index
}

func OpenIndex(path string) (*Index, error) {

	mapping := buildMapping()
	index, err := bleve.New(path, mapping)

	if err != bleve.ErrorIndexPathExists && err != nil {
		return nil, err
	}

	return &Index{index: index}, nil
}

func (i *Index) Add(key string, val interface{}) {
	i.index.Index(key, val)
}
