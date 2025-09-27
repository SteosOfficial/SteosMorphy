
DICT_PATH = ../analyzer/morph.dawg

run: test
	STEOSMORPHY_DICT_PATH=$(DICT_PATH) ./test

test: main.c libanalyzer.a libanalyzer.h
	gcc -o $@ $< -I. -L. -lanalyzer

libanalyzer.a libanalyzer.h: wrapper.go
	go build -buildmode=c-archive -o libanalyzer.a wrapper.go

clean:
	rm -f libanalyzer.a libanalyzer.h test
