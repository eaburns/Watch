package main

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"code.google.com/p/goplan9/plan9/acme"
)

type winUi struct {
	win *acme.Win
	rr chan struct{}
}

func newWin(watchPath string) (ui, error) {
	win, err := acme.New()
	if err != nil {
		return nil, err
	}

	wd, err := os.Getwd()
	if err != nil {
		log.Println("Failed to get the current directory, not setting dumpdir:", err)
	} else if err := win.Ctl("dumpdir %s", wd); err != nil {
		log.Println("Failed to set the dumpdir:", err)
	}

	if err := win.Ctl("dump %s", strings.Join(os.Args, " ")); err != nil {
		log.Println("Failed to set the dump command:", err)
	}

	abs, err := filepath.Abs(watchPath)
	if err != nil {
		return nil, errors.New("Failed getting the absolute path of " + watchPath + ": " + err.Error())
	}
	if err := win.Name(abs + "/+watch"); err != nil {
		return nil, errors.New("Failed to set the win name: " + err.Error())
	}

	win.Ctl("clean")
	win.Fprintf("tag", "Get ")

	rerun := make(chan struct{})
	go func(rerun chan<-struct{}){
		for e := range win.EventChan() {
			if e.C2 != 'x' && e.C2 != 'X' {
				continue
			}

			switch string(e.Text) {
			case "Get":
				rerun <- struct{}{}

			case "Del":
				if err := win.Ctl("delete"); err != nil {
					log.Fatalln("Failed to delete the window:", err)
				}

			default:
				win.WriteEvent(e)
			}
		}
		os.Exit(0)
	}(rerun)

	return winUi{ win, rerun }, nil
}

func (w winUi) rerun() <-chan struct{} {
	return w.rr
}

func (w winUi) redisplay(f func(io.Writer)) {
	w.win.Addr(",")
	w.win.Write("data", nil)

	f(bodyWriter{w.win})

	w.win.Fprintf("addr", "#0")
	w.win.Ctl("dot=addr")
	w.win.Ctl("show")
	w.win.Ctl("clean")
}

type bodyWriter struct {
	*acme.Win
}

func (b bodyWriter) Write(data []byte) (int, error) {
	// maxWrite is the maximum amount of data written at a time to an win's body.
	const maxWrite = 1024

	sz := len(data)
	for len(data) > 0 {
		n := maxWrite
		if len(data) < n {
			n = len(data)
		}
		m, err := b.Win.Write("body", data[:n])
		if err != nil {
			return m, err
		}
		data = data[m:]
	}
	return sz, nil
}