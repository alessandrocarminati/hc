MAJOR=$(shell ./verscripts/maj.sh)
MINOR=$(shell ./verscripts/min.sh)
CHASH=$(shell git log --pretty=oneline| head -n1 |cut -d" " -f1)
DIRTY=$(shell ./verscripts/dirty.sh)
SOURCES := $(wildcard *.go)

all: hc-$(MAJOR).$(MINOR)

generated.go: template.html
	@echo "package main" >generated.go
	@echo "var tmplStr = \`" >>generated.go
	@cat template.html >>generated.go
	@echo "\`" >>generated.go

hc-$(MAJOR).$(MINOR): $(SOURCES) generated.go
	go build -ldflags "-w -X 'main.Version=$(MAJOR)' -X 'main.Build=$(MINOR)' -X 'main.Hash=$(CHASH)' -X 'main.Dirty=$(DIRTY)'" -o  hc-$(MAJOR).$(MINOR)

clean:
	rm -rf  hc-* generated.go
