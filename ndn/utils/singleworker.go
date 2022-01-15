package utils

import (
	"sync"
)

const (
	MAX_QUEUED_JOBS = 1024
)

type SingleWorker struct {
	quit			chan bool
	jobs			chan Job	// queued jobs
	running			Job
	numqjobs		int			// # of queued jobs
	mutex			sync.Mutex
	killed			bool		// is the worker killed?
}

func NewSingleWorker() JobSubmitter {
	w := &SingleWorker{
		quit:		make(chan bool, 1 ),
		jobs:		make(chan Job, MAX_QUEUED_JOBS),
		numqjobs:	0,
		killed:		true,
	}
	go w.run()
	return w
}

func (w *SingleWorker) run() {
	w.killed = false
	for j := range w.jobs {
		w.setRunning(j)
		j.Execute()
		w.setRunning(nil)
	}
}

func (w *SingleWorker) setRunning(j Job) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.running = j
	if j != nil {
		w.numqjobs -= 1
	} 
}

func (w *SingleWorker) Stop() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.killed = true
	close(w.quit)
	if w.running != nil {
		if j, ok := w.running.(Killable); ok {
			j.Kill()
		}
	}
}

func (w *SingleWorker) Submit(j Job) bool {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	if w.killed {
		return false
	}

	if w.numqjobs >= MAX_QUEUED_JOBS {
		return false
	}

	w.numqjobs += 1
	w.jobs <- j
	return true
}

