package main

import (
	"encoding/json"
	"html/template"
	"net/http"
	"log"

	"github.com/gorilla/mux"
)

type PageData struct {
	Title   string
	LogTree *LogTree
}

func http_present(h *History, port *string, ver string) {

	logTree, err := buildLogTree(h.RawLog)
	h.LogTree = logTree
	if err != nil {
		log.Fatalf("failed to build log tree: %v", err)
	}

	tpl, err := template.New("webpage").Parse(tmplStr)
	if err != nil {
		log.Fatalf("failed to parse template: %v", err)
	}

	router := mux.NewRouter()
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		data := PageData{
			Title:   ver,
			LogTree: logTree,
		}
		tpl.Execute(w, data)
	}).Methods("GET")

	router.HandleFunc("/logs", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(logTree)
	}).Methods("GET")

	http.Handle("/", router)
	log.Println("Listening on :" + *port)
	http.ListenAndServe(":" + *port, nil)
}
