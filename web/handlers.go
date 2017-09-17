package web

import (
	"net/http"
)

func (a *App) Index(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("Hello world"))
}
