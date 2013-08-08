package main

import (
	"errors"
	"flag"
	"io"
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
	term  = flag.Bool("t", false, "Just run in the terminal (instead of an acme win)")
)

const rebuildDelay = 200*time.Millisecond

type ui interface {
	redisplay(func(io.Writer))
	// An empty struct is sent when the command should be rerun.
	rerun() <-chan struct{}
}

type writerUi struct {
	io.Writer
	rr chan struct{} // nothing ever sent on this channel.
}

func (w writerUi) redisplay(f func(io.Writer)) {
	f(w)
}

func (w writerUi) rerun() <-chan struct{} {
	return w.rr
}

func main() {
	flag.Parse()

	watchPath := "."

	ui := ui(writerUi{os.Stdout, make(chan struct{})})
	if !*term {
		var err error
		if ui, err = newWin(watchPath); err != nil {
			log.Fatalln("Failed to open a win:", err)
		}
	}

	timer := time.NewTimer(0)
	changes := startWatching(watchPath)
	lastRun := time.Time{}
	lastChange := time.Now()

	for {
		select {
		case lastChange = <-changes:
			timer.Reset(rebuildDelay)

		case <-timer.C:
			if lastRun.Before(lastChange) {
				lastRun = run(ui)
			}
		}
	}
}

func run(ui ui) time.Time {
	r, w, err := os.Pipe()
	if err != nil {
		log.Fatalln("Failed to create a pipe:", err)
	}

	cmd := exec.Command(flag.Arg(0), flag.Args()[1:]...)
	cmd.Stdout = w
	cmd.Stderr = w

	ui.redisplay(func(out io.Writer) {
		io.WriteString(out, strings.Join(flag.Args(), " ")+"\n")
		go func() {
			if _, err := io.Copy(out, r); err != nil {
				log.Fatalln("Failed to copy command output to the display:", err)
			}
		}()
		if err := cmd.Run(); err != nil {
			io.WriteString(out, err.Error()+"\n")
		}
		io.WriteString(out, time.Now().String()+"\n")
	})

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

		case ev := <-w.Event:
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
	s, err := os.Stat(p)
	switch {
	case os.IsNotExist(err):
		q := path.Dir(p)
		if q == p {
			err := errors.New("Failed to find directory for removed file " + p)
			return time.Time{}, err
		}
		return modTime(q)

	case err != nil:
		return time.Time{}, err
	}
	return s.ModTime(), nil
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
	s, err := os.Stat(p)
	switch {
	case os.IsNotExist(err):
		return false, nil
	case err != nil:
		return false, err
	}
	return s.IsDir(), nil
}

func debugPrint(f string, vals ...interface{}) {
	if *debug {
		log.Printf("DEBUG: "+f, vals...)
	}
}
