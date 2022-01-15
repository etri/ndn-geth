/*
 * Copyright (c) 2019-2021,  HII of ETRI.
 *
 * This file is part of geth-ndn (Go Ethereum client for NDN).
 * author: tqtung@gmail.com 
 *
 * geth-ndn is free software: you can redistribute it and/or modify it under the terms
 * of the GNU General Public License as published by the Free Software Foundation,
 * either version 3 of the License, or (at your option) any later version.
 *
 * geth-ndn is distributed in the hope that it will be useful, but WITHOUT ANY WARRANTY;
 * without even the implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR
 * PURPOSE.  See the GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License along with
 * geth-ndn, e.g., in COPYING.md file.  If not, see <http://www.gnu.org/licenses/>.
 */


package utils

import (
	//"fmt"
	"sync"
//	"github.com/ethereum/go-ethereum/log"
)

type Job interface {
	Execute()
}

type Killable interface {
	Kill()
}

type Worker struct {
	jobCh	chan Job //input job chanel
	running	Job //currently running job
	quit	chan bool
	wg		*sync.WaitGroup
	mutex	sync.Mutex
}
type JobSubmitter interface {
	Stop()
	Submit(Job) bool
}
type workerPool struct {
	workerCh	chan chan Job //queue of workers' job queue
	workers		[]*Worker
	jobCh		chan Job //input job queue
	numworkers	int
//	quit		chan bool
	wg			sync.WaitGroup
	mutex		sync.Mutex
	closed		bool
}

func NewWorkerPool(num int) JobSubmitter {
	pool :=  &workerPool {
		numworkers: num,
		workerCh: make(chan chan Job, num),
		jobCh:	make(chan Job, 1),
//		quit:	make(chan bool),
		closed:	false,
	}
	workers := make([]*Worker, pool.numworkers)
	for i:=0; i< pool.numworkers; i++ {
		workers[i] = &Worker{
			jobCh: 	make(chan Job,1),
			wg:		&pool.wg,
			quit:	make(chan bool),
		}
	}
	pool.workers = workers

	go pool.run()
	return pool
}


func (wp *workerPool) run() {
//	log.Info("Run worker pool")
//	defer log.Info("Exit worker pool")
	
	for i:=0; i< wp.numworkers; i++ {
		wp.workers[i].start(wp.workerCh)
	}

	for wch := range wp.workerCh {
		job, ok := <- wp.jobCh
		if !ok {
			break //job channel has been closed
		}
		//otherwise send job to worker
		wch <- job	
	}
}

//add a job to the pool for executing
func (wp *workerPool) Submit(job Job) bool {
	wp.mutex.Lock()

	if wp.closed { //pool already destroyed
		wp.mutex.Unlock()
		return false
	}
	
	defer wp.mutex.Unlock()

	//fmt.Println("Worker pool receving job")
	wp.jobCh <- job	
	//fmt.Println("Worker pool has sent the job")
	return true
}

func (wp *workerPool) Stop() {
	wp.mutex.Lock()
	if wp.closed {
		wp.mutex.Unlock()
		return
	}

	wp.closed = true //make sure no more job can be added
	wp.mutex.Unlock()

	//do not receive any more job
	//pool main loop will terminate
	close(wp.jobCh)

	for _, w := range wp.workers {
		w.stop()
	}
//	log.Info("wait for all workers to stop")
	//wait for all workers stop
	wp.wg.Wait()
//	log.Info("All workers has done")
	//just close the worker chanel
	close(wp.workerCh) 
}

func (w *Worker) start(c chan chan Job) {
	go	w.work(c) 
}

func (w *Worker) work(c chan chan Job) {
	w.wg.Add(1)
	defer w.wg.Done()
	//fmt.Println("Worker starts working")
	LOOP:
	for {
		//register my job channel
		c <- w.jobCh //never blocked
		select {
		case job := <- w.jobCh:
			w.setRunningJob(job)
			//fmt.Println("Worker receive job, execute now")
			job.Execute()
			w.setRunningJob(nil)
		case <- w.quit:
			break LOOP
		}
	}
}
func (w *Worker) setRunningJob(job Job) {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	w.running = job
}
func (w *Worker) stop() {
	w.mutex.Lock()
	defer w.mutex.Unlock()
	//close(w.jobCh)
	if w.running !=nil {
		if tokill, ok := w.running.(Killable); ok {
			tokill.Kill() //try to interupt current job execution
		}
	}
	close(w.quit)
}
