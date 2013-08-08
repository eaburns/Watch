package main

import (
	"errors"
	"flag"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"code.google.com/p/go.exp/fsnotify"
)

var (
	debug = flag.Bool("d", false, "Enable debugging output")
)

func main() {
	flag.Parse()

	tick := time.NewTicker(time.Second)
	changes := startWatching(".")

	lastRun := run()
	lastChange := time.Time{}

	for {
		select {
		case lastChange = <-changes:

		case <-tick.C:
			if lastRun.Before(lastChange) {
				lastRun = run()
			}
		}
	}
}

func run() time.Time {
	os.Stdout.WriteString(strings.Join(flag.Args(), " ") + "\n")

	cmd := exec.Command(flag.Arg(0), flag.Args()[1:]...)
	out, err := cmd.CombinedOutput()
	os.Stdout.WriteString(string(out))
	if err != nil {
		os.Stdout.WriteString(err.Error() + "\n")
	}
	os.Stdout.WriteString(time.Now().String() + "\n")

	return time.Now()
}

func startWatching(p string) <-chan time.Time {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}

	switch isdir, err := isDir(p); {
	case err != nil:
		log.Fatalf("Failed to watch %s: %s", p, err)
	case isdir:
		watchDir(w, p)
	default:
		watch(w, p)
	}

	changes := make(chan time.Time)

	go sendChanges(w, changes)

	return changes
}

func sendChanges(w *fsnotify.Watcher, changes chan<- time.Time) {
	for {
		select {
		case err := <-w.Error:
			log.Fatalf("Watcher error: %s\n", err)

		case ev := <- w.Event:
			time, err := modTime(ev.Name)
			if err != nil {
				log.Printf("Failed to get even time: %s", err)
				continue
			}

			debugPrint("%s at %s", ev, time)

			if ev.IsCreate() {
				switch isdir, err := isDir(ev.Name); {
				case err != nil:
					log.Printf("Couldn't check if %s is a directory: %s", ev.Name, err)
					continue

				case isdir:
					watchDir(w, ev.Name)
				}
			}

			changes <- time
		}
	}
}

func modTime(p string) (time.Time, error) {
	switch s, err := os.Stat(p); {
	case os.IsNotExist(err):
		q := path.Dir(p)
		if q == p {
			err := errors.New("Failed to find directory for removed file " + p) 
			return time.Time{}, err
		}
		return modTime(q)

	case err != nil:
		return time.Time{}, err

	default:
		return s.ModTime(), nil
	}
}

func watchDir(w *fsnotify.Watcher, p string) {
	ents, err := ioutil.ReadDir(p)
	switch {
	case os.IsNotExist(err):
		return

	case err != nil:
		log.Printf("Failed to watch %s: %s", p, err)
	}

	for _, e := range ents {
		sub := path.Join(p, e.Name())	
		isdir, err := isDir(sub)
		if err != nil {
			log.Printf("Failed to watch %s: %s", sub, err)
		}

		if isdir {
			watchDir(w, sub)
		}
	}

	watch(w, p)
}

func watch(w *fsnotify.Watcher, p string) {
	debugPrint("Watching %s", p)

	switch err := w.Watch(p); {
	case os.IsNotExist(err):
		debugPrint("%s no longer exists", p)

	case err != nil:
		log.Printf("Failed to watch %s: %s", p, err)
	}
}

func isDir(p string) (bool, error) {
	switch s, err := os.Stat(p); {
	case os.IsNotExist(err):
		return false, nil
	case err != nil:
		return false, err
	default:
		return s.IsDir(), nil
	}
}

func debugPrint(f string, vals ...interface{}) {
	if *debug {
		log.Printf("DEBUG: " + f, vals...)
	}
}
