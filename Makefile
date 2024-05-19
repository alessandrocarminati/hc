MAJOR=$(shell ./verscripts/maj.sh)
MINOR=$(shell ./verscripts/min.sh)
CHASH=$(shell git log --pretty=oneline| head -n1 |cut -d" " -f1)
DIRTY=$(shell ./verscripts/dirty.sh)
SOURCES := $(wildcard *.go)

all: hc-$(MAJOR).$(MINOR)

hc-$(MAJOR).$(MINOR): $(SOURCES)
	go build -ldflags "-w -X 'main.Version=$(MAJOR)' -X 'main.Build=$(MINOR)' -X 'main.Hash=$(CHASH)' -X 'main.Dirty=$(DIRTY)'" -o  hc-$(MAJOR).$(MINOR)

clean:
	rm -rf  hc-*
