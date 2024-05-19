package main

import (
	"encoding/json"
	"html/template"
	"net/http"
	"log"

	"github.com/gorilla/mux"
)

var tpl = template.Must(template.ParseFiles("template.html"))

type PageData struct {
	Title   string
	LogTree *LogTree
}

func http_present(logs *[]string, port *string, ver string) {

	logTree, err := buildLogTree(*logs)
	if err != nil {
		log.Fatalf("failed to build log tree: %v", err)
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
